import assert from 'node:assert/strict'
import { execFileSync } from 'node:child_process'
import { createServer } from 'node:http'
import { chromium, request } from 'playwright'

const webBaseURL = process.env.AGENT_BOARD_E2E_WEB_URL || 'http://127.0.0.1:3000'
const apiBaseURL = process.env.AGENT_BOARD_E2E_API_URL || 'http://127.0.0.1:3001'
const authStorageKey = 'agent-board.auth'

const providerModelId = 'e2e-mention-model'
const dockerNetwork = process.env.AGENT_BOARD_E2E_DOCKER_NETWORK || 'agent-board_default'
const dockerGateway = execFileSync(
  'docker',
  ['network', 'inspect', '--format', '{{(index .IPAM.Config 0).Gateway}}', dockerNetwork],
  { encoding: 'utf8' }
).trim()
assert.ok(dockerGateway, 'compose E2E Docker network has no gateway')

const providerServer = createServer((req, res) => {
  if (req.method === 'GET' && req.url === '/models') {
    res.writeHead(200, { 'content-type': 'application/json' })
    res.end(JSON.stringify({ data: [{ id: providerModelId, name: 'E2E Mention Model' }] }))
    return
  }
  res.writeHead(404)
  res.end()
})
await new Promise((resolve, reject) => {
  providerServer.once('error', reject)
  providerServer.listen(0, '0.0.0.0', resolve)
})
const providerAddress = providerServer.address()
assert.ok(providerAddress && typeof providerAddress === 'object', 'mention Provider fixture did not bind a TCP port')
const providerBaseURL = 'http://' + dockerGateway + ':' + providerAddress.port


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

async function waitForProviderHealth(projectId, providerId, token, expected = 'HEALTHY') {
  let last
  for (let attempt = 0; attempt < 50; attempt += 1) {
    last = await call('GET', '/api/projects/' + projectId + '/providers/' + providerId, { token })
    if (last.healthStatus === expected) return last
    if (last.healthStatus === 'UNHEALTHY') break
    await new Promise(resolve => setTimeout(resolve, 100))
  }
  assert.equal(last?.healthStatus, expected, 'Provider health did not reach ' + expected)
  return last
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

  const mentionProvider = await call('POST', '/api/projects/' + project.id + '/providers', {
    token: creator.tokens.accessToken,
    body: {
      name: 'Mention E2E Provider',
      kind: 'e2e-mock',
      baseUrl: providerBaseURL,
      enabled: true,
      safeMetadata: {}
    },
    status: 201
  })
  await waitForProviderHealth(project.id, mentionProvider.id, creator.tokens.accessToken)

  const mentionModel = await call('POST', '/api/projects/' + project.id + '/model-profiles', {
    token: creator.tokens.accessToken,
    body: {
      providerId: mentionProvider.id,
      name: 'Mention E2E Model',
      model: providerModelId,
      generationSettings: {},
      enabled: true
    },
    status: 201
  })
  const mentionAgent = await call('POST', '/api/projects/' + project.id + '/agents', {
    token: creator.tokens.accessToken,
    body: {
      name: 'Mention E2E Target',
      roleInstructions: 'Perform only explicitly delegated Issue work.',
      engine: 'opencode',
      modelProfileId: mentionModel.id,
      engineSettings: {},
      concurrencyLimit: 1,
      allowDelegation: false,
      state: 'ENABLED'
    },
    status: 201
  })
  assert.ok(mentionAgent.id)

  {
    const { context, page } = await pageFor(collaborator.tokens)
    await openProjectPage(page, project.id, `/projects/${project.id}/issues/${issue.id}`)
    await page.getByRole('heading', { name: 'Discussion & activity' }).waitFor()

    const mentionBody = 'Browser structured mention proof; plain @Mention E2E Target prose is inert.'
    await page.getByRole('button', { name: 'Mention Agent' }).click()
    await page.getByPlaceholder('Filter Agents…').fill('Mention E2E Target')
    const previewResponsePromise = page.waitForResponse(response => (
      response.url().includes('/api/projects/' + project.id + '/issues/' + issue.id + '/comments/trigger-preview')
      && response.request().method() === 'POST'
      && response.request().postDataJSON()?.mentionAgentIds?.includes(mentionAgent.id)
    ), { timeout: 15_000 })
    await page.getByRole('button', { name: '@Mention E2E Target' }).click()
    const previewResponse = await previewResponsePromise
    assert.equal(previewResponse.status(), 200, 'real mention preview request failed')
    assert.deepEqual(previewResponse.request().postDataJSON().mentionAgentIds, [mentionAgent.id])
    const preview = await previewResponse.json()
    assert.equal(preview.mentions.length, 1)
    assert.equal(preview.mentions[0].targetAgentId, mentionAgent.id)
    assert.equal(preview.mentions[0].eligible, true)
    assert.equal(preview.mentions[0].reasonCode, null)
    assert.equal(preview.implicit, null)
    await page.locator('[aria-label="Agent mention preview"]').getByText('@Mention E2E Target · Eligible to request work').waitFor()

    await page.locator('textarea').fill(mentionBody)
    const mentionPostPromise = page.waitForResponse(response => (
      response.url().includes('/api/projects/' + project.id + '/issues/' + issue.id + '/comments')
      && !response.url().includes('/trigger-preview')
      && response.request().method() === 'POST'
    ), { timeout: 15_000 })
    await page.getByRole('button', { name: 'Post comment' }).click()
    const mentionPostResponse = await mentionPostPromise
    assert.equal(mentionPostResponse.status(), 201, 'real structured mention comment POST failed')
    const mentionRequest = mentionPostResponse.request().postDataJSON()
    assert.deepEqual(mentionRequest.mentionAgentIds, [mentionAgent.id])
    assert.match(mentionRequest.requestId, /^[0-9a-f-]{36}$/i)
    const postedMentionComment = await mentionPostResponse.json()
    assert.equal(postedMentionComment.body, mentionBody)
    assert.equal(postedMentionComment.mentions.length, 1)
    assert.equal(postedMentionComment.mentions[0].targetAgentId, mentionAgent.id)
    assert.equal(postedMentionComment.mentions[0].outcome, 'QUEUED')
    assert.ok(postedMentionComment.mentions[0].delegationId)
    assert.ok(postedMentionComment.mentions[0].delegatedRunId)

    let mentionArticle = page.locator('article').filter({ hasText: mentionBody })
    await mentionArticle.getByText('@Mention E2E Target', { exact: true }).waitFor()
    await mentionArticle.getByText('Work queued').waitFor()
    const delegatedRunLink = mentionArticle.getByRole('link', { name: 'Open delegated Run' })
    await delegatedRunLink.waitFor()
    assert.equal(
      await delegatedRunLink.getAttribute('href'),
      '/projects/' + project.id + '/runs/' + postedMentionComment.mentions[0].delegatedRunId
    )

    let mentionComments = await call('GET', '/api/projects/' + project.id + '/issues/' + issue.id + '/comments', {
      token: collaborator.tokens.accessToken
    })
    const durableMention = mentionComments.find(comment => comment.id === postedMentionComment.id)
    assert.ok(durableMention, 'structured mention comment was not durable')
    assert.equal(durableMention.mentions.length, 1)
    assert.equal(durableMention.mentions[0].targetAgentId, mentionAgent.id)
    assert.equal(durableMention.mentions[0].outcome, 'QUEUED')
    assert.equal(durableMention.mentions[0].delegatedRunId, postedMentionComment.mentions[0].delegatedRunId)

    await page.getByRole('button', { name: 'Mention Agent' }).click()
    await page.getByPlaceholder('Filter Agents…').fill('Mention E2E Target')
    const busyPreviewPromise = page.waitForResponse(response => (
      response.url().includes('/api/projects/' + project.id + '/issues/' + issue.id + '/comments/trigger-preview')
      && response.request().method() === 'POST'
      && response.request().postDataJSON()?.mentionAgentIds?.includes(mentionAgent.id)
    ), { timeout: 15_000 })
    await page.getByRole('button', { name: '@Mention E2E Target' }).click()
    const busyPreviewResponse = await busyPreviewPromise
    assert.equal(busyPreviewResponse.status(), 200, 'real busy mention preview request failed')
    const busyPreview = await busyPreviewResponse.json()
    assert.equal(busyPreview.mentions.length, 1)
    assert.equal(busyPreview.mentions[0].targetAgentId, mentionAgent.id)
    assert.equal(busyPreview.mentions[0].eligible, false)
    assert.equal(busyPreview.mentions[0].reasonCode, 'TARGET_BUSY')
    assert.equal(busyPreview.implicit, null)
    await page.locator('[aria-label="Agent mention preview"]').getByText('@Mention E2E Target · Agent already has active work on this Issue').waitFor()

    const blockedBody = 'Browser blocked structured mention proof.'
    await page.locator('textarea').fill(blockedBody)
    const blockedPostPromise = page.waitForResponse(response => (
      response.url().includes('/api/projects/' + project.id + '/issues/' + issue.id + '/comments')
      && !response.url().includes('/trigger-preview')
      && response.request().method() === 'POST'
    ), { timeout: 15_000 })
    await page.getByRole('button', { name: 'Post comment' }).click()
    const blockedPostResponse = await blockedPostPromise
    assert.equal(blockedPostResponse.status(), 201, 'real blocked mention comment POST failed')
    const blockedComment = await blockedPostResponse.json()
    assert.equal(blockedComment.mentions.length, 1)
    assert.equal(blockedComment.mentions[0].targetAgentId, mentionAgent.id)
    assert.equal(blockedComment.mentions[0].outcome, 'BLOCKED')
    assert.equal(blockedComment.mentions[0].reasonCode, 'TARGET_BUSY')
    assert.equal(blockedComment.mentions[0].delegatedRunId, null)
    const blockedArticle = page.locator('article').filter({ hasText: blockedBody })
    await blockedArticle.getByText('@Mention E2E Target', { exact: true }).waitFor()
    await blockedArticle.getByText('Agent already has active work on this Issue').waitFor()
    assert.equal(await blockedArticle.getByRole('link', { name: 'Open delegated Run' }).count(), 0)

    await page.reload({ waitUntil: 'domcontentloaded' })
    await page.getByText(mentionBody).waitFor()
    mentionArticle = page.locator('article').filter({ hasText: mentionBody })
    await mentionArticle.getByText('@Mention E2E Target', { exact: true }).waitFor()
    await mentionArticle.getByText('Work queued').waitFor()
    assert.equal(
      await mentionArticle.getByRole('link', { name: 'Open delegated Run' }).getAttribute('href'),
      '/projects/' + project.id + '/runs/' + postedMentionComment.mentions[0].delegatedRunId
    )
    await page.getByText(blockedBody).waitFor()
    await page.locator('article').filter({ hasText: blockedBody }).getByText('Agent already has active work on this Issue').waitFor()

    mentionComments = await call('GET', '/api/projects/' + project.id + '/issues/' + issue.id + '/comments', {
      token: collaborator.tokens.accessToken
    })
    assert.equal(
      mentionComments.filter(comment => comment.id === postedMentionComment.id)[0]?.mentions[0]?.delegatedRunId,
      postedMentionComment.mentions[0].delegatedRunId,
      'delegated Run linkage did not survive reload'
    )

    await page.locator('textarea').fill('Durable **root** comment')
    await page.getByRole('button', { name: 'Post comment' }).click()
    await page.getByText('Durable root comment').waitFor()

    let rootArticle = page.locator('article').filter({ hasText: 'Durable root comment' })
    await rootArticle.getByRole('button', { name: 'Reply' }).click()
    await page.getByText('Replying to authz-collaborator').waitFor()
    await page.locator('textarea').fill('Durable reply')
    await page.getByRole('button', { name: 'Post reply' }).click()
    await page.getByText('Durable reply').waitFor()

    let comments = await call('GET', `/api/projects/${project.id}/issues/${issue.id}/comments`, { token: collaborator.tokens.accessToken })
    const rootComment = comments.find(comment => comment.body === 'Durable **root** comment')
    const replyComment = comments.find(comment => comment.body === 'Durable reply')
    assert.ok(rootComment, 'comment API did not persist the lifecycle root comment')
    assert.ok(replyComment, 'comment API did not persist the lifecycle reply')
    assert.equal(replyComment.parentCommentId, rootComment.id, 'reply parent relation was not durable')
    const rootCommentId = rootComment.id

    await call('PATCH', `/api/projects/${project.id}/issues/${issue.id}/comments/${rootCommentId}`, {
      token: creator.tokens.accessToken,
      body: { body: 'creator must not edit this' },
      status: 403
    })
    await call('DELETE', `/api/projects/${project.id}/issues/${issue.id}/comments/${rootCommentId}`, {
      token: creator.tokens.accessToken,
      status: 403
    })

    rootArticle = page.locator('article').filter({ hasText: 'Durable root comment' })
    await rootArticle.getByRole('button', { name: 'Edit' }).click()
    const editingRootArticle = page.locator('article').filter({ has: page.locator('textarea') })
    await editingRootArticle.locator('textarea').fill('Durable edited root')
    await editingRootArticle.getByRole('button', { name: 'Save edit' }).click()
    await page.getByText('Durable edited root').waitFor()
    await page.getByText('· edited', { exact: true }).waitFor()

    rootArticle = page.locator('article').filter({ hasText: 'Durable edited root' })
    await rootArticle.getByRole('button', { name: 'Resolve' }).click()
    await rootArticle.getByText(/Resolved by authz-collaborator/).waitFor()

    await rootArticle.getByRole('button', { name: '❤️' }).click()
    await rootArticle.getByRole('button', { name: '❤️ 1' }).waitFor()

    await page.reload({ waitUntil: 'domcontentloaded' })
    await page.getByText('Durable edited root').waitFor()
    rootArticle = page.locator('article').filter({ hasText: 'Durable edited root' })
    await rootArticle.getByText(/Resolved by authz-collaborator/).waitFor()
    await rootArticle.getByRole('button', { name: '❤️ 1' }).waitFor()

    comments = await call('GET', `/api/projects/${project.id}/issues/${issue.id}/comments`, { token: collaborator.tokens.accessToken })
    const persistedRoot = comments.find(comment => comment.id === rootCommentId)
    assert.equal(persistedRoot?.body, 'Durable edited root', 'edited comment did not survive reload')
    assert.ok(persistedRoot?.resolvedAt, 'resolved timestamp did not persist')
    assert.equal(persistedRoot?.resolvedBy.id, collaborator.user.id, 'resolver identity was not durable')
    assert.equal(persistedRoot?.reactions.length, 1, 'reaction did not persist')
    assert.equal(persistedRoot?.reactions[0].count, 1, 'reaction count was not idempotent')
    assert.equal(persistedRoot?.reactions[0].reactedByCurrentUser, true, 'reaction actor projection was incorrect')

    await rootArticle.getByRole('button', { name: '❤️ 1' }).click()
    await rootArticle.getByRole('button', { name: 'Reopen' }).click()
    await rootArticle.getByRole('button', { name: 'Delete' }).click()
    await page.getByText('Comment deleted').waitFor()
    await page.getByText('Durable reply').waitFor()

    comments = await call('GET', `/api/projects/${project.id}/issues/${issue.id}/comments`, { token: collaborator.tokens.accessToken })
    const tombstonedRoot = comments.find(comment => comment.id === rootCommentId)
    const persistedReply = comments.find(comment => comment.body === 'Durable reply')
    assert.ok(tombstonedRoot, 'tombstoning removed the parent comment')
    assert.ok(persistedReply, 'tombstoning a parent destroyed its reply')
    assert.equal(tombstonedRoot.body, null, 'deleted parent body remained exposed')
    assert.ok(tombstonedRoot.deletedAt, 'deleted parent did not expose tombstone state')
    assert.equal(tombstonedRoot.reactions.length, 0, 'tombstoned comment retained reactions')
    assert.equal(tombstonedRoot.resolvedAt, null, 'tombstoned comment retained resolution')
    assert.equal(persistedReply.parentCommentId, rootCommentId, 'tombstoning changed reply parent identity')

    await page.reload({ waitUntil: 'domcontentloaded' })
    await page.getByText('Comment deleted').waitFor()
    await page.getByText('Durable reply').waitFor()
    await page.getByText('Replying to authz-collaborator').waitFor()
    await context.close()
  }

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
    assert.equal(await page.getByRole('button', { name: 'Post comment' }).count(), 0, 'viewer saw comment mutation control')
    assert.equal(await page.getByRole('button', { name: 'Reply' }).count(), 0, 'viewer saw reply mutation control')
    assert.equal(await page.getByRole('button', { name: 'Edit' }).count(), 0, 'viewer saw comment edit control')
    assert.equal(await page.getByRole('button', { name: 'Delete' }).count(), 0, 'viewer saw comment delete control')
    assert.equal(await page.getByRole('button', { name: 'Resolve' }).count(), 0, 'viewer saw discussion resolution control')
    assert.equal(await page.getByRole('button', { name: 'Reopen' }).count(), 0, 'viewer saw discussion reopen control')
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
  providerServer.closeAllConnections()
  await new Promise(resolve => providerServer.close(resolve))
  await browser.close()
  await api.dispose()
}
