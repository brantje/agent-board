import assert from 'node:assert/strict'
import { chromium, request } from 'playwright'

const webBaseURL = process.env.AGENT_BOARD_E2E_WEB_URL || 'http://127.0.0.1:3000'
const apiBaseURL = process.env.AGENT_BOARD_E2E_API_URL || 'http://127.0.0.1:3001'
const authStorageKey = 'agent-board.auth'

const passwords = {
  admin: 'Admin-password-123!',
  creator: 'Creator-password-123!',
  collaborator: 'Collaborator-password-123!'
}

const api = await request.newContext({ baseURL: apiBaseURL })
const browser = await chromium.launch({ headless: true })

async function call(method, path, { token, body, status = 200 } = {}) {
  const expected = Array.isArray(status) ? status : [status]
  const response = await api.fetch(path, {
    method,
    failOnStatusCode: false,
    headers: token ? { Authorization: `Bearer ${token}` } : undefined,
    data: body
  })
  const text = await response.text()
  assert.ok(expected.includes(response.status()), `${method} ${path}: status ${response.status()}, expected ${expected.join('/')}; body=${text}`)
  if (!text) return undefined
  try {
    return JSON.parse(text)
  } catch {
    return text
  }
}

async function login(loginValue, password) {
  return call('POST', '/api/auth/login', { body: { login: loginValue, password }, status: 200 })
}

async function createActiveMember(adminToken, username, password) {
  const pending = await call('POST', '/api/auth/users', {
    token: adminToken,
    body: {
      username,
      email: `${username}@example.test`,
      displayName: username
    },
    status: 201
  })
  await call('POST', '/api/auth/setup/complete', {
    body: { token: pending.setupToken, password },
    status: 200
  })
  return { user: pending.user, tokens: await login(username, password) }
}

async function effectiveRole(projectId, token, expected) {
  const value = await call('GET', `/api/projects/${projectId}/access/effective-role`, { token })
  assert.equal(value.role, expected)
  return value.role
}

async function pageFor(tokens) {
  const context = await browser.newContext()
  const stored = {
    accessToken: tokens.accessToken,
    accessTokenExpiresAt: tokens.accessTokenExpiresAt,
    refreshToken: tokens.refreshToken,
    refreshTokenExpiresAt: tokens.refreshTokenExpiresAt
  }
  await context.addInitScript(({ key, value }) => {
    localStorage.setItem(key, JSON.stringify(value))
  }, { key: authStorageKey, value: stored })
  const page = await context.newPage()
  return { context, page }
}

async function openProjectPage(page, projectId, path) {
  const roleResponse = page.waitForResponse(response => (
    response.url().includes(`/api/projects/${projectId}/access/effective-role`) && response.ok()
  ), { timeout: 15_000 })
  await page.goto(`${webBaseURL}${path}`, { waitUntil: 'domcontentloaded' })
  await page.locator('h1').first().waitFor({ timeout: 15_000 })
  await roleResponse
}

try {
  const bootstrap = await call('GET', '/api/auth/bootstrap')
  assert.equal(bootstrap.available, true, 'compose E2E must start from a fresh user database')

  await call('POST', '/api/auth/bootstrap/register', {
    body: {
      username: 'authz-admin',
      email: 'authz-admin@example.test',
      displayName: 'Authorization Admin',
      password: passwords.admin
    },
    status: 201
  })
  const admin = await login('authz-admin', passwords.admin)
  assert.equal(admin.user.deploymentRole, 'admin')

  const creator = await createActiveMember(admin.accessToken, 'authz-creator', passwords.creator)
  const collaborator = await createActiveMember(admin.accessToken, 'authz-collaborator', passwords.collaborator)
  assert.equal(creator.tokens.user.deploymentRole, 'member')
  assert.equal(collaborator.tokens.user.deploymentRole, 'member')

  const project = await call('POST', '/api/projects', {
    token: creator.tokens.accessToken,
    body: {
      name: 'Private Authorization Project',
      issuePrefix: 'E2E',
      sourceType: 'local',
      repositoryPath: '/repositories/.authorization-e2e',
      defaultBranch: 'main',
      allowInternalRunner: true,
      workflowSettings: {}
    },
    status: 201
  })
  assert.ok(project.id)
  await effectiveRole(project.id, creator.tokens.accessToken, 'admin')

  const hiddenProjects = await call('GET', '/api/projects', { token: collaborator.tokens.accessToken })
  assert.equal(hiddenProjects.some(item => item.id === project.id), false, 'private Project leaked into unrelated member listing')
  await call('GET', `/api/projects/${project.id}`, { token: collaborator.tokens.accessToken, status: 404 })

  await call('PUT', `/api/projects/${project.id}/access/users/${collaborator.user.id}`, {
    token: creator.tokens.accessToken,
    body: { role: 'viewer' }
  })
  await effectiveRole(project.id, collaborator.tokens.accessToken, 'viewer')
  await call('POST', `/api/projects/${project.id}/issues`, {
    token: collaborator.tokens.accessToken,
    body: { title: 'Viewer must not create this' },
    status: 403
  })

  {
    const { context, page } = await pageFor(collaborator.tokens)
    await openProjectPage(page, project.id, `/projects/${project.id}/board`)
    await page.getByRole('heading', { name: /Private Authorization Project \/ Board/ }).waitFor()
    assert.equal(await page.getByRole('button', { name: 'New issue' }).count(), 0, 'viewer saw Issue creation control')
    assert.equal(await page.locator(`a[href="/projects/${project.id}/settings"]`).count(), 0, 'viewer saw Project Settings navigation')
    assert.equal(await page.locator('a[href="/settings"]').count(), 0, 'deployment member saw global Settings navigation')
    await context.close()
  }

  const group = await call('POST', '/api/groups', {
    token: admin.accessToken,
    body: { name: 'authorization-collaborators' },
    status: 201
  })
  await call('POST', `/api/groups/${group.id}/members`, {
    token: admin.accessToken,
    body: { userID: collaborator.user.id },
    status: 204
  })
  await call('PUT', `/api/projects/${project.id}/access/groups/${group.id}`, {
    token: creator.tokens.accessToken,
    body: { role: 'member' }
  })
  await effectiveRole(project.id, collaborator.tokens.accessToken, 'member')

  const issue = await call('POST', `/api/projects/${project.id}/issues`, {
    token: collaborator.tokens.accessToken,
    body: { title: 'Member-created Issue' },
    status: 201
  })
  assert.ok(issue.id)

  {
    const { context, page } = await pageFor(collaborator.tokens)
    await openProjectPage(page, project.id, `/projects/${project.id}/board`)
    await page.getByRole('button', { name: 'New issue' }).waitFor()
    assert.equal(await page.locator(`a[href="/projects/${project.id}/settings"]`).count(), 0, 'member saw Project Settings navigation')
    await context.close()
  }

  await call('PUT', `/api/projects/${project.id}/access/groups/${group.id}`, {
    token: creator.tokens.accessToken,
    body: { role: 'viewer' }
  })
  await effectiveRole(project.id, collaborator.tokens.accessToken, 'viewer')

  {
    const { context, page } = await pageFor(collaborator.tokens)
    await openProjectPage(page, project.id, `/projects/${project.id}/issues/${issue.id}`)
    await page.getByText('Member-created Issue').first().waitFor()
    assert.equal(await page.getByRole('button', { name: 'Edit issue' }).count(), 0, 'viewer saw Issue edit control')
    assert.equal(await page.getByRole('button', { name: 'Assign Agent' }).count(), 0, 'viewer saw assignment control')
    await context.close()
  }

  await call('PUT', `/api/projects/${project.id}/access/groups/${group.id}`, {
    token: creator.tokens.accessToken,
    body: { role: 'admin' }
  })
  await effectiveRole(project.id, collaborator.tokens.accessToken, 'admin')

  await call('GET', '/api/groups', { token: collaborator.tokens.accessToken, status: 403 })
  await call('GET', '/api/runners', { token: collaborator.tokens.accessToken, status: 403 })

  await call('PUT', `/api/projects/${project.id}/access/groups/${group.id}`, {
    token: creator.tokens.accessToken,
    body: { role: 'viewer' }
  })
  await call('PUT', `/api/projects/${project.id}/access/users/${collaborator.user.id}`, {
    token: creator.tokens.accessToken,
    body: { role: 'admin' }
  })
  await effectiveRole(project.id, collaborator.tokens.accessToken, 'admin')
  const renamed = await call('PATCH', `/api/projects/${project.id}`, {
    token: collaborator.tokens.accessToken,
    body: { name: 'Private Authorization Project Updated' }
  })
  assert.equal(renamed.name, 'Private Authorization Project Updated')

  {
    const { context, page } = await pageFor(collaborator.tokens)
    await openProjectPage(page, project.id, `/projects/${project.id}/board`)
    await page.locator(`a[href="/projects/${project.id}/settings"]`).waitFor()
    assert.equal(await page.locator('a[href="/settings"]').count(), 0, 'Project admin deployment member saw global Settings')
    await context.close()
  }

  const extraSession = await login('authz-collaborator', passwords.collaborator)
  const sessionsBefore = await call('GET', '/api/auth/me/sessions', { token: extraSession.accessToken })
  assert.ok(sessionsBefore.length >= 2, `expected multiple collaborator sessions, got ${sessionsBefore.length}`)
  await call('DELETE', `/api/auth/me/sessions/${sessionsBefore[0].id}`, { token: extraSession.accessToken, status: 204 })
  const sessionsAfter = await call('GET', '/api/auth/me/sessions', { token: extraSession.accessToken })
  assert.equal(sessionsAfter.length, sessionsBefore.length - 1, 'session revoke did not update active session list')

  await call('POST', '/api/auth/logout', { body: { refreshToken: collaborator.tokens.refreshToken }, status: 204 })
  await call('POST', '/api/auth/refresh', { body: { refreshToken: collaborator.tokens.refreshToken }, status: 401 })

  const beforeDisable = await login('authz-collaborator', passwords.collaborator)
  await call('POST', `/api/auth/users/${collaborator.user.id}/disable`, { token: admin.accessToken })
  await call('GET', `/api/projects/${project.id}`, { token: beforeDisable.accessToken, status: 401 })
  await call('POST', `/api/auth/users/${collaborator.user.id}/enable`, { token: admin.accessToken })

  const afterEnable = await login('authz-collaborator', passwords.collaborator)
  await effectiveRole(project.id, afterEnable.accessToken, 'admin')
  const preservedIssue = await call('GET', `/api/projects/${project.id}/issues/${issue.id}`, { token: afterEnable.accessToken })
  assert.equal(preservedIssue.id, issue.id, 're-enabled user lost Project history/access')

  console.log('Playwright authorization flow passed')
} finally {
  await browser.close()
  await api.dispose()
}
