import { mount } from '@vue/test-utils'
import { describe, expect, it } from 'vitest'
import ActivityTimeline from '../app/components/ActivityTimeline.vue'
import { projectRunActivity, eventActivityIcon, eventTitle, toolActivityIcon, toolActivityLabel, toolActivityTarget } from '../app/utils/events'
import { event } from './execution-fixtures'
import { uiStubs } from './ui-stubs'

describe('run activity projection', () => {
  it('projects reasoning and pairs tool lifecycle events by toolCallId', () => {
    const events = [
      event({ id: 'thought', type: 'agent.message', sequence: 1, payload: { kind: 'reasoning', message: 'Inspect the handler first.' } }),
      event({ id: 'started', type: 'tool.started', sequence: 2, payload: { toolCallId: 'call-1', name: 'read', input: { filePath: 'server/internal/handler/issue.go' } } }),
      event({ id: 'completed', type: 'tool.completed', sequence: 3, payload: { toolCallId: 'call-1', name: 'read', resultPreview: 'func Create()' } })
    ]
    const items = projectRunActivity(events)
    expect(items).toHaveLength(2)
    expect(items[0]).toMatchObject({ kind: 'thought', message: 'Inspect the handler first.' })
    expect(items[1]).toMatchObject({ kind: 'tool', label: 'Read', target: 'server/internal/handler/issue.go', status: 'completed', resultPreview: 'func Create()' })
    expect(items.filter(item => item.kind === 'tool')).toHaveLength(1)
  })

  it('projects visible agent messages separately from thoughts and pairs historical tools without toolCallId', () => {
    const items = projectRunActivity([
      event({ id: 'note', type: 'agent.message', sequence: 1, payload: { kind: 'message', message: 'Node is missing. Try another approach.' } }),
      event({ id: 'start', type: 'tool.started', sequence: 2, payload: { name: 'bash', source: 'opencode' } }),
      event({ id: 'done', type: 'tool.completed', sequence: 3, payload: { name: 'bash', source: 'opencode', summary: 'ls -la /workspace', resultPreview: '(no output)' } })
    ])
    expect(items).toHaveLength(2)
    expect(items[0]).toMatchObject({ kind: 'message', message: 'Node is missing. Try another approach.' })
    expect(items[1]).toMatchObject({ kind: 'tool', label: 'Bash', target: 'ls -la /workspace', status: 'completed', resultPreview: '(no output)' })
  })

  it('keeps run, runtime, and engine lifecycle events in the feed', () => {
    const items = projectRunActivity([
      event({ id: 'run', type: 'run.started', sequence: 1, occurredAt: '2026-09-09T10:23:29.308593Z' }),
      event({ id: 'prov', type: 'runtime.provisioning', sequence: 2, occurredAt: '2026-09-09T10:23:29.312183Z' }),
      event({ id: 'rt', type: 'runtime.started', sequence: 3, occurredAt: '2026-09-09T10:23:29.734848Z' }),
      event({
        id: 'server',
        type: 'tool.started',
        sequence: 4,
        occurredAt: '2026-09-09T10:23:29.738411Z',
        payload: { name: 'opencode-server', command: ['opencode', 'serve', '--hostname', '127.0.0.1', '--port', '4096'] }
      }),
      event({ id: 'resume', type: 'run.resumed', sequence: 5 }),
      event({ id: 'wait', type: 'run.waiting_for_input', sequence: 6 }),
      event({ id: 'decision', type: 'decision.recorded', sequence: 7 })
    ])
    expect(items.map(item => item.kind === 'tool' ? item.label : item.kind === 'event' ? item.title : item.kind)).toEqual([
      'Run Started',
      'Runtime Provisioning',
      'Runtime Started',
      'Opencode-server',
      'Run Resumed',
      'Run Waiting For Input',
      'Decision Recorded'
    ])
    expect(items[3]).toMatchObject({
      kind: 'tool',
      target: 'opencode serve --hostname 127.0.0.1 --port 4096',
      status: 'running'
    })
  })

  it('projects workspace transfer lifecycle events with runner names and coalesces progress', () => {
    const items = projectRunActivity([
      event({ id: 'xfer-start', type: 'workspace.transfer.started', sequence: 1, payload: { direction: 'to_runner', runnerId: 'runner-1', runnerName: 'Internal runner', transferId: 'xfer-1' } }),
      event({ id: 'xfer-progress', type: 'workspace.transfer.progress', sequence: 2, payload: { direction: 'to_runner', runnerName: 'Internal runner', transferId: 'xfer-1', bytesTransferred: 65536, totalBytes: 131072 } }),
      event({ id: 'xfer-done', type: 'workspace.transfer.completed', sequence: 3, payload: { direction: 'to_runner', runnerName: 'Internal runner', transferId: 'xfer-1', bytesTransferred: 131072, totalBytes: 131072 } }),
      event({ id: 'sync-fail', type: 'workspace.transfer.failed', sequence: 4, payload: { direction: 'from_runner', runnerName: 'Internal runner', transferId: 'xfer-2', reason: 'runner disconnected' } })
    ])
    expect(items).toHaveLength(2)
    expect(items.map(item => item.kind === 'event' ? item.title : item.kind)).toEqual([
      'Workspace transferred to Internal runner',
      'Failed to synchronize changes from Internal runner'
    ])
    expect(items[0]).toMatchObject({ description: '128.0 KiB (100%)' })
    expect(items[1]).toMatchObject({ description: 'runner disconnected' })
    expect(eventTitle(event({
      type: 'workspace.transfer.started',
      payload: { direction: 'to_runner', runnerName: 'lab-host' }
    }))).toBe('Preparing workspace')
    expect(eventTitle(event({
      type: 'workspace.transfer.progress',
      payload: { direction: 'to_runner', runnerName: 'lab-host' }
    }))).toBe('Transferring workspace to lab-host')
    expect(eventTitle(event({
      type: 'workspace.transfer.started',
      payload: { direction: 'from_runner', runnerName: 'lab-host' }
    }))).toBe('Synchronizing changes from lab-host')
    expect(eventActivityIcon('workspace.transfer.started')).toBe('i-lucide-folder-sync')
  })

  it('projects an intentional process stop as stopped rather than failed', () => {
    const items = projectRunActivity([
      event({
        id: 'start',
        type: 'tool.started',
        sequence: 1,
        payload: { name: 'opencode-server', command: ['opencode', 'serve', '--hostname', '127.0.0.1', '--port', '4096'] }
      }),
      event({
        id: 'stop',
        type: 'tool.stopped',
        sequence: 2,
        payload: { kind: 'tool', name: 'opencode-server', command: ['opencode', 'serve', '--hostname', '127.0.0.1', '--port', '4096'], exitCode: 137 }
      })
    ])
    expect(items).toHaveLength(1)
    expect(items[0]).toMatchObject({
      kind: 'tool',
      name: 'opencode-server',
      label: 'Opencode-server',
      target: 'opencode serve --hostname 127.0.0.1 --port 4096',
      status: 'stopped'
    })
  })

  it('projects historical process-stop failures as stopped', () => {
    const items = projectRunActivity([
      event({
        id: 'start',
        type: 'tool.started',
        sequence: 1,
        payload: { name: 'opencode-server', command: ['opencode', 'serve', '--hostname', '127.0.0.1', '--port', '4096'] }
      }),
      event({
        id: 'fail',
        type: 'tool.failed',
        sequence: 2,
        payload: {
          result: { kind: 'tool', name: 'opencode-server', command: ['opencode', 'serve', '--hostname', '127.0.0.1', '--port', '4096'] },
          error: 'process exited with code -1'
        }
      })
    ])
    expect(items).toHaveLength(1)
    expect(items[0]).toMatchObject({
      kind: 'tool',
      name: 'opencode-server',
      label: 'Opencode-server',
      target: 'opencode serve --hostname 127.0.0.1 --port 4096',
      status: 'stopped'
    })
    expect(items[0]).not.toMatchObject({ reason: 'process exited with code -1' })
  })

  it('keeps genuine tool crashes failed', () => {
    const items = projectRunActivity([
      event({ id: 'a', type: 'tool.started', sequence: 1, payload: { toolCallId: 'a', name: 'edit', input: { path: 'a.go' } } }),
      event({ id: 'b', type: 'tool.started', sequence: 2, payload: { toolCallId: 'b', name: 'edit', input: { path: 'b.go' } } }),
      event({ id: 'af', type: 'tool.failed', sequence: 3, payload: { toolCallId: 'a', name: 'edit', reason: 'write failed' } })
    ])
    expect(items).toHaveLength(2)
    expect(items[0]).toMatchObject({ kind: 'tool', toolCallId: 'a', target: 'a.go', status: 'failed', reason: 'write failed' })
    expect(items[1]).toMatchObject({ kind: 'tool', toolCallId: 'b', target: 'b.go', status: 'running' })
  })

  it('pairs question lifecycle events and resolves selected option labels', () => {
    const items = projectRunActivity([
      event({
        id: 'question',
        type: 'question.created',
        sequence: 1,
        payload: {
          questionId: 'q1',
          prompt: 'Which marker should I write?',
          kind: 'SINGLE_CHOICE',
          options: [{ id: 'alpha', label: 'alpha' }, { id: 'beta', label: 'beta' }]
        }
      }),
      event({
        id: 'answer',
        type: 'question.answered',
        sequence: 2,
        payload: { questionId: 'q1', answer: { kind: 'SINGLE_CHOICE', optionIds: ['beta'] } }
      })
    ])
    expect(items).toHaveLength(1)
    expect(items[0]).toMatchObject({
      kind: 'question',
      questionId: 'q1',
      prompt: 'Which marker should I write?',
      choiceKind: 'SINGLE_CHOICE',
      status: 'answered',
      answer: 'beta',
      options: [{ id: 'alpha', label: 'alpha' }, { id: 'beta', label: 'beta' }]
    })
  })

  it('attaches the question and selected answers to Decision Recorded', () => {
    const items = projectRunActivity([
      event({
        id: 'question',
        type: 'question.created',
        sequence: 1,
        payload: {
          questionId: 'q1',
          prompt: 'Which markers should I write?',
          kind: 'MULTI_CHOICE',
          options: [{ id: 'alpha', label: 'alpha' }, { id: 'beta', label: 'beta' }, { id: 'gamma', label: 'gamma' }]
        }
      }),
      event({
        id: 'answer',
        type: 'question.answered',
        sequence: 2,
        payload: { questionId: 'q1', answer: { kind: 'MULTI_CHOICE', optionIds: ['beta', 'gamma'] } }
      }),
      event({
        id: 'decision',
        type: 'decision.recorded',
        sequence: 3,
        payload: { decisionId: 'd1', questionId: 'q1', kind: 'QUESTION_ANSWER', outcome: '["beta","gamma"]' }
      })
    ])
    expect(items).toHaveLength(2)
    expect(items[1]).toMatchObject({
      kind: 'event',
      title: 'Decision Recorded',
      description: 'Which markers should I write? — beta, gamma'
    })
  })

  it('formats known and unknown tools and targets without dumping arbitrary input', () => {
    expect(toolActivityLabel('grep')).toBe('Search')
    expect(toolActivityLabel('custom_tool')).toBe('Custom tool')
    expect(toolActivityIcon('bash')).toBe('i-lucide-square-terminal')
    expect(toolActivityIcon('read')).toBe('i-lucide-file-text')
    expect(eventActivityIcon('runtime.provisioning')).toBe('i-lucide-box')
    expect(eventActivityIcon('run.started')).toBe('i-lucide-play')
    expect(toolActivityTarget({ command: ['go', 'test', './...'] })).toBe('go test ./...')
    expect(toolActivityTarget({ nested: { huge: true } })).toBe('')
  })

  it('shows thoughts before tools when persistence arrived tool-then-thought', () => {
    const items = projectRunActivity([
      event({ id: 'write-css', type: 'tool.started', sequence: 1, payload: { toolCallId: 'css', name: 'write', input: { path: '/workspace/styles.css' } } }),
      event({ id: 'done-css', type: 'tool.completed', sequence: 2, payload: { toolCallId: 'css', name: 'write', summary: 'styles.css' } }),
      event({ id: 'thought-css', type: 'agent.message', sequence: 3, payload: { kind: 'reasoning', message: 'Now let me create the CSS file for styling.' } }),
      event({ id: 'write-json', type: 'tool.started', sequence: 4, payload: { toolCallId: 'json', name: 'write', input: { path: '/workspace/games.json' } } }),
      event({ id: 'done-json', type: 'tool.completed', sequence: 5, payload: { toolCallId: 'json', name: 'write', summary: 'games.json' } }),
      event({ id: 'thought-json', type: 'agent.message', sequence: 6, payload: { kind: 'message', message: 'Now let me create the games.json file.' } })
    ])
    expect(items.map(item => item.kind)).toEqual(['thought', 'tool', 'message', 'tool'])
    expect(items[0]).toMatchObject({ kind: 'thought', message: 'Now let me create the CSS file for styling.' })
    expect(items[1]).toMatchObject({ kind: 'tool', target: 'styles.css', status: 'completed' })
    expect(items[2]).toMatchObject({ kind: 'message', message: 'Now let me create the games.json file.' })
    expect(items[3]).toMatchObject({ kind: 'tool', target: 'games.json', status: 'completed' })
  })

  it('moves only the adjacent thought before a tool when multiple thoughts follow it', () => {
    const items = projectRunActivity([
      event({ id: 'write-css', type: 'tool.started', sequence: 1, payload: { toolCallId: 'css', name: 'write', input: { path: '/workspace/styles.css' } } }),
      event({ id: 'done-css', type: 'tool.completed', sequence: 2, payload: { toolCallId: 'css', name: 'write', summary: 'styles.css' } }),
      event({ id: 'thought-css', type: 'agent.message', sequence: 3, payload: { kind: 'reasoning', message: 'Now let me create the CSS file for styling.' } }),
      event({ id: 'thought-json', type: 'agent.message', sequence: 4, payload: { kind: 'reasoning', message: 'Next I will create games.json.' } })
    ])
    expect(items.map(item => item.kind)).toEqual(['thought', 'tool', 'thought'])
    expect(items[0]).toMatchObject({ message: 'Now let me create the CSS file for styling.' })
    expect(items[1]).toMatchObject({ kind: 'tool', target: 'styles.css', status: 'completed' })
    expect(items[2]).toMatchObject({ message: 'Next I will create games.json.' })
  })

  it('keeps thoughts that already precede their tool', () => {
    const items = projectRunActivity([
      event({ id: 'thought', type: 'agent.message', sequence: 1, payload: { kind: 'reasoning', message: 'Inspect the handler first.' } }),
      event({ id: 'started', type: 'tool.started', sequence: 2, payload: { toolCallId: 'call-1', name: 'read', input: { filePath: 'server/internal/handler/issue.go' } } }),
      event({ id: 'completed', type: 'tool.completed', sequence: 3, payload: { toolCallId: 'call-1', name: 'read', resultPreview: 'func Create()' } })
    ])
    expect(items.map(item => item.kind)).toEqual(['thought', 'tool'])
    expect(items[0]).toMatchObject({ message: 'Inspect the handler first.' })
  })

  it('leaves a trailing thought after lifecycle events unchanged', () => {
    const items = projectRunActivity([
      event({ id: 'run', type: 'run.started', sequence: 1 }),
      event({ id: 'thought', type: 'agent.message', sequence: 2, payload: { kind: 'reasoning', message: 'Starting work.' } })
    ])
    expect(items.map(item => item.kind)).toEqual(['event', 'thought'])
  })

  it('hides engine question reply-accepted and binding-resolved events', () => {
    const items = projectRunActivity([
      event({ id: 'wait', type: 'run.waiting_for_input', sequence: 1 }),
      event({ id: 'accepted', type: 'engine.question_reply_accepted', sequence: 2, payload: { questionId: 'q1' } }),
      event({ id: 'resolved', type: 'engine.question_binding_resolved', sequence: 3, payload: { questionId: 'q1' } }),
      event({ id: 'resume', type: 'run.resumed', sequence: 4 })
    ])
    expect(items).toHaveLength(2)
    expect(items.map(item => item.kind === 'event' ? item.event.type : item.kind)).toEqual([
      'run.waiting_for_input',
      'run.resumed'
    ])
  })

  it('hides created files while keeping other file activity', () => {
    const items = projectRunActivity([
      event({ id: 'created', type: 'file.created', sequence: 1, payload: { path: 'games.json' } }),
      event({ id: 'modified', type: 'file.modified', sequence: 2, payload: { path: 'styles.css' } }),
      event({ id: 'engine-created', type: 'engine.file_created', sequence: 3, payload: { path: 'index.html' } })
    ])
    expect(items).toHaveLength(1)
    expect(items[0]).toMatchObject({ kind: 'event', event: { type: 'file.modified' }, description: 'styles.css' })
  })

  it('projects Todowrite input.todos into structured todos with Todo label and count', () => {
    const todos = [
      { content: 'Create project structure', status: 'in progress', priority: 'high' },
      { content: 'Build core habit tracking', status: 'pending', priority: 'high' },
      { content: 'Polish UI/UX', status: 'completed', priority: 'medium' }
    ]
    const items = projectRunActivity([
      event({
        id: 'start',
        type: 'tool.started',
        sequence: 1,
        payload: { toolCallId: 'todo-1', name: 'Todowrite', input: { todos } }
      }),
      event({
        id: 'done',
        type: 'tool.completed',
        sequence: 2,
        payload: { toolCallId: 'todo-1', name: 'Todowrite', summary: '6 todos' }
      })
    ])
    expect(items).toHaveLength(1)
    expect(items[0]).toMatchObject({
      kind: 'tool',
      label: 'Todo',
      target: '1/3',
      status: 'completed',
      todos: [
        { content: 'Create project structure', status: 'in_progress' },
        { content: 'Build core habit tracking', status: 'pending' },
        { content: 'Polish UI/UX', status: 'completed' }
      ]
    })
  })

  it('prefers resultPreview todos over stale started input on completion', () => {
    const items = projectRunActivity([
      event({
        id: 'start',
        type: 'tool.started',
        sequence: 1,
        payload: {
          toolCallId: 'todo-1',
          name: 'Todowrite',
          input: { todos: [{ content: 'Create project structure', status: 'in progress' }] }
        }
      }),
      event({
        id: 'done',
        type: 'tool.completed',
        sequence: 2,
        payload: {
          toolCallId: 'todo-1',
          name: 'Todowrite',
          resultPreview: JSON.stringify([{ content: 'Create project structure', status: 'completed' }])
        }
      })
    ])
    expect(items[0]).toMatchObject({
      kind: 'tool',
      target: '1/1',
      todos: [{ content: 'Create project structure', status: 'completed' }]
    })
  })

  it('updates earlier Todowrite rows when a later call changes todo statuses', () => {
    const items = projectRunActivity([
      event({
        id: 'start-1',
        type: 'tool.started',
        sequence: 1,
        payload: {
          toolCallId: 'todo-a',
          name: 'Todowrite',
          input: { todos: [{ content: 'Ship feature', status: 'in progress' }] }
        }
      }),
      event({ id: 'done-1', type: 'tool.completed', sequence: 2, payload: { toolCallId: 'todo-a', name: 'Todowrite' } }),
      event({
        id: 'start-2',
        type: 'tool.started',
        sequence: 3,
        payload: {
          toolCallId: 'todo-b',
          name: 'Todowrite',
          input: { todos: [{ content: 'Ship feature', status: 'completed' }, { content: 'Add tests', status: 'in progress' }] }
        }
      }),
      event({ id: 'done-2', type: 'tool.completed', sequence: 4, payload: { toolCallId: 'todo-b', name: 'Todowrite' } })
    ])
    expect(items).toHaveLength(2)
    expect(items[0]).toMatchObject({
      kind: 'tool',
      target: '1/2',
      todos: [
        { content: 'Ship feature', status: 'completed' },
        { content: 'Add tests', status: 'in_progress' }
      ]
    })
    expect(items[1]).toMatchObject({
      kind: 'tool',
      target: '1/2',
      todos: [
        { content: 'Ship feature', status: 'completed' },
        { content: 'Add tests', status: 'in_progress' }
      ]
    })
  })

  it('parses todos from resultPreview when input is missing', () => {
    const items = projectRunActivity([
      event({
        id: 'done',
        type: 'tool.completed',
        sequence: 1,
        payload: {
          toolCallId: 'todo-2',
          name: 'todo_write',
          resultPreview: JSON.stringify([{ content: 'Ship feature', status: 'completed' }])
        }
      })
    ])
    expect(items[0]).toMatchObject({
      kind: 'tool',
      label: 'Todo',
      target: '1/1',
      todos: [{ content: 'Ship feature', status: 'completed' }]
    })
  })

  it('keeps two sequential Todowrite calls as separate rows', () => {
    const items = projectRunActivity([
      event({
        id: 'start-1',
        type: 'tool.started',
        sequence: 1,
        payload: { toolCallId: 'todo-a', name: 'Todowrite', input: { todos: [{ content: 'First list', status: 'pending' }] } }
      }),
      event({ id: 'done-1', type: 'tool.completed', sequence: 2, payload: { toolCallId: 'todo-a', name: 'Todowrite' } }),
      event({
        id: 'start-2',
        type: 'tool.started',
        sequence: 3,
        payload: { toolCallId: 'todo-b', name: 'Todowrite', input: { todos: [{ content: 'Second list', status: 'pending' }] } }
      }),
      event({ id: 'done-2', type: 'tool.completed', sequence: 4, payload: { toolCallId: 'todo-b', name: 'Todowrite' } })
    ])
    expect(items).toHaveLength(2)
    expect(items[0]).toMatchObject({ kind: 'tool', todos: [{ content: 'First list', status: 'pending' }] })
    expect(items[1]).toMatchObject({ kind: 'tool', todos: [{ content: 'Second list', status: 'pending' }] })
  })
})

describe('ActivityTimeline run feed', () => {
  it('renders run and runtime lifecycle events in the compact activity row style', () => {
    const items = projectRunActivity([
      event({ id: 'run', type: 'run.started', sequence: 1, occurredAt: '2026-09-09T10:23:29.308593Z' }),
      event({ id: 'prov', type: 'runtime.provisioning', sequence: 2, occurredAt: '2026-09-09T10:23:29.312183Z' }),
      event({
        id: 'server',
        type: 'tool.started',
        sequence: 3,
        payload: { name: 'opencode-server', command: ['opencode', 'serve', '--hostname', '127.0.0.1', '--port', '4096'] }
      })
    ])
    const wrapper = mount(ActivityTimeline, {
      props: { items },
      global: { stubs: uiStubs }
    })
    const runtime = wrapper.get('[data-activity-kind="event"]')
    expect(runtime.find('[data-icon="i-lucide-chevron-right"]').exists()).toBe(true)
    expect(wrapper.find('[data-icon="i-lucide-play"]').exists()).toBe(true)
    expect(wrapper.find('[data-icon="i-lucide-box"]').exists()).toBe(true)
    expect(wrapper.text()).toContain('Run Started')
    expect(wrapper.text()).toContain('Runtime Provisioning')
    expect(wrapper.text()).toContain('opencode serve --hostname 127.0.0.1 --port 4096')
    expect(wrapper.text()).not.toContain('2026-09-09T10:23:29')
    const times = wrapper.findAll('time')
    expect(times).toHaveLength(3)
    expect(times[0]?.attributes('datetime')).toBe('2026-09-09T10:23:29.308593Z')
    expect(times[0]?.classes()).toContain('text-muted')
    expect(times[0]?.element.previousElementSibling?.textContent).toContain('Run Started')
  })
  it('renders reasoning with the brain icon, messages with the speech icon, and reveals tool results on click', async () => {
    const items = projectRunActivity([
      event({ id: 'thought', type: 'agent.message', sequence: 1, payload: { kind: 'reasoning', message: 'Inspect handlers.' } }),
      event({ id: 'note', type: 'agent.message', sequence: 2, payload: { kind: 'message', message: 'Node/npx is not available.' } }),
      event({ id: 'start', type: 'tool.started', sequence: 3, payload: { toolCallId: 'call', name: 'read', input: { filePath: 'issue.go' } } }),
      event({ id: 'done', type: 'tool.completed', sequence: 4, payload: { toolCallId: 'call', name: 'read', resultPreview: 'func Create()' } })
    ])
    const wrapper = mount(ActivityTimeline, {
      props: { items },
      global: { stubs: uiStubs }
    })
    expect(wrapper.get('[data-activity-kind="thought"]').find('[data-icon="i-lucide-brain"]').exists()).toBe(true)
    expect(wrapper.get('[data-activity-kind="message"]').find('[data-icon="i-lucide-message-circle"]').exists()).toBe(true)
    expect(wrapper.findAll('[data-icon="i-lucide-brain"]')).toHaveLength(1)
    expect(wrapper.text()).toContain('Inspect handlers.')
    expect(wrapper.text()).toContain('Node/npx is not available.')
    expect(wrapper.text()).toContain('Read')
    expect(wrapper.find('[data-icon="i-lucide-file-text"]').exists()).toBe(true)
    expect(wrapper.text()).toContain('issue.go')
    expect(wrapper.text()).not.toContain('result: func Create()')
    await wrapper.get('[data-tool-status="completed"] button').trigger('click')
    expect(wrapper.text()).toContain('result: func Create()')
    expect(wrapper.findAll('li')).toHaveLength(3)
  })

  it('renders stopped status for an intentionally stopped process', () => {
    const items = projectRunActivity([
      event({
        id: 'start',
        type: 'tool.started',
        sequence: 1,
        payload: { name: 'opencode-server', command: ['opencode', 'serve', '--hostname', '127.0.0.1', '--port', '4096'] }
      }),
      event({
        id: 'stop',
        type: 'tool.stopped',
        sequence: 2,
        payload: { kind: 'tool', name: 'opencode-server', command: ['opencode', 'serve', '--hostname', '127.0.0.1', '--port', '4096'], exitCode: 137 }
      })
    ])
    const wrapper = mount(ActivityTimeline, {
      props: { items },
      global: { stubs: uiStubs }
    })
    const tool = wrapper.get('[data-tool-status="stopped"]')
    expect(tool.text()).toContain('Opencode-server')
    expect(tool.text()).toContain('stopped')
    expect(tool.text()).not.toContain('failed')
    expect(tool.find('.text-error').exists()).toBe(false)
  })

  it('renders nested process-failure tools with the process name and command', () => {
    const items = projectRunActivity([
      event({
        id: 'start',
        type: 'tool.started',
        sequence: 1,
        payload: { name: 'opencode-server', command: ['opencode', 'serve', '--port', '4096'] }
      }),
      event({
        id: 'fail',
        type: 'tool.failed',
        sequence: 2,
        payload: {
          result: { kind: 'tool', name: 'opencode-server', command: ['opencode', 'serve', '--port', '4096'] },
          error: 'process exited with code 137'
        }
      })
    ])
    const wrapper = mount(ActivityTimeline, {
      props: { items },
      global: { stubs: uiStubs }
    })
    const tool = wrapper.get('[data-tool-status="stopped"]')
    expect(tool.text()).toContain('Opencode-server')
    expect(tool.text()).toContain('opencode serve --port 4096')
    expect(tool.text()).toContain('stopped')
    expect(tool.text()).not.toContain('failed')
    expect(wrapper.text()).not.toContain('Tool')
  })

  it('renders failed status on the row and shows the result after expanding', async () => {
    const items = projectRunActivity([
      event({ id: 'start', type: 'tool.started', sequence: 1, payload: { toolCallId: 'call', name: 'edit', input: { path: 'issue.go' } } }),
      event({ id: 'failed', type: 'tool.failed', sequence: 2, payload: { toolCallId: 'call', name: 'edit', resultPreview: 'partial update', reason: 'write failed' } })
    ])
    const wrapper = mount(ActivityTimeline, {
      props: { items },
      global: { stubs: uiStubs }
    })
    const tool = wrapper.get('[data-tool-status="failed"]')
    expect(tool.text()).toContain('failed')
    expect(tool.text()).not.toContain('result: partial update')
    await tool.get('button').trigger('click')
    expect(tool.text()).toContain('result: partial update')
    expect(tool.text()).toContain('error: write failed')
  })

  it('renders given options and the answer for answered choice questions', () => {
    const items = projectRunActivity([
      event({
        id: 'question',
        type: 'question.created',
        sequence: 1,
        payload: {
          questionId: 'q1',
          prompt: 'Which marker should I write?',
          kind: 'SINGLE_CHOICE',
          options: [{ id: 'alpha', label: 'alpha' }, { id: 'beta', label: 'beta' }]
        }
      }),
      event({ id: 'answer', type: 'question.answered', sequence: 2, payload: { questionId: 'q1', answer: { kind: 'SINGLE_CHOICE', optionIds: ['beta'] } } })
    ])
    const wrapper = mount(ActivityTimeline, {
      props: { items },
      global: { stubs: uiStubs }
    })
    const question = wrapper.get('[data-question-status="answered"]')
    expect(question.text()).toContain('Which marker should I write?')
    expect(question.text()).toContain('Given options')
    expect(question.text()).toContain('SINGLE CHOICE')
    expect(question.text()).toContain('alpha')
    expect(question.text()).toContain('beta')
    expect(question.text()).toContain('answer: beta')
    expect(question.find('summary').exists()).toBe(false)
  })

  it('renders given options for open choice questions', () => {
    const items = projectRunActivity([
      event({
        id: 'question',
        type: 'question.created',
        sequence: 1,
        payload: {
          questionId: 'q1',
          prompt: 'What features do you want in the HTML5 games website?',
          kind: 'SINGLE_CHOICE',
          options: [{ id: 'library', label: 'Game library/browser' }, { id: 'arcade', label: 'Arcade cabinet UI' }]
        }
      })
    ])
    const wrapper = mount(ActivityTimeline, {
      props: { items },
      global: { stubs: uiStubs }
    })
    const question = wrapper.get('[data-question-status="open"]')
    expect(question.text()).toContain('Given options')
    expect(question.text()).toContain('SINGLE CHOICE')
    expect(question.text()).toContain('Game library/browser')
    expect(question.text()).toContain('Arcade cabinet UI')
    expect(question.text()).toContain('waiting for answer')
  })

  it('renders Todowrite as a Cursor-style checklist without JSON result', () => {
    const todos = [
      { content: 'Create project structure', status: 'in progress', priority: 'high' },
      { content: 'Build core habit tracking', status: 'pending', priority: 'high' },
      { content: 'Polish UI/UX', status: 'completed', priority: 'medium' }
    ]
    const items = projectRunActivity([
      event({
        id: 'start',
        type: 'tool.started',
        sequence: 1,
        payload: { toolCallId: 'todo-1', name: 'Todowrite', input: { todos } }
      }),
      event({
        id: 'done',
        type: 'tool.completed',
        sequence: 2,
        payload: { toolCallId: 'todo-1', name: 'Todowrite', summary: '6 todos' }
      })
    ])
    const wrapper = mount(ActivityTimeline, {
      props: { items },
      global: { stubs: uiStubs }
    })
    const tool = wrapper.get('[data-tool-kind="todo"]')
    expect(tool.text()).toContain('Todo')
    expect(tool.text()).toContain('1/3')
    expect(tool.text()).toContain('Create project structure')
    expect(tool.text()).toContain('Build core habit tracking')
    expect(tool.text()).toContain('Polish UI/UX')
    expect(tool.text()).not.toContain('result:')
    expect(tool.text()).not.toContain('Todowrite')
    expect(tool.text()).not.toContain('priority')
    expect(tool.find('[data-icon="i-lucide-list-todo"]').exists()).toBe(true)
    expect(tool.find('[data-todo-status="in_progress"] [data-icon="i-lucide-loader-circle"]').exists()).toBe(true)
    expect(tool.find('[data-todo-status="completed"] .line-through').exists()).toBe(true)
    expect(tool.find('[data-todo-status="in_progress"] .animate-spin').exists()).toBe(true)
    expect(tool.find('button').exists()).toBe(false)
    expect(tool.find('.run-activity-todo-list').exists()).toBe(true)
  })

  it('renders a failed todo tool reason below the checklist', () => {
    const items = projectRunActivity([
      event({
        id: 'start',
        type: 'tool.started',
        sequence: 1,
        payload: { toolCallId: 'todo-1', name: 'Todowrite', input: { todos: [{ content: 'Ship feature', status: 'in progress' }] } }
      }),
      event({
        id: 'failed',
        type: 'tool.failed',
        sequence: 2,
        payload: { toolCallId: 'todo-1', name: 'Todowrite', reason: 'todo write failed' }
      })
    ])
    const wrapper = mount(ActivityTimeline, {
      props: { items },
      global: { stubs: uiStubs }
    })
    const tool = wrapper.get('[data-tool-kind="todo"]')
    expect(tool.attributes('data-tool-status')).toBe('failed')
    expect(tool.text()).toContain('failed')
    expect(tool.text()).toContain('Ship feature')
    expect(tool.text()).toContain('error: todo write failed')
  })

  it('renders two Todowrite rows independently', () => {
    const items = projectRunActivity([
      event({
        id: 'start-1',
        type: 'tool.started',
        sequence: 1,
        payload: { toolCallId: 'todo-a', name: 'Todowrite', input: { todos: [{ content: 'First list', status: 'pending' }] } }
      }),
      event({ id: 'done-1', type: 'tool.completed', sequence: 2, payload: { toolCallId: 'todo-a', name: 'Todowrite' } }),
      event({
        id: 'start-2',
        type: 'tool.started',
        sequence: 3,
        payload: { toolCallId: 'todo-b', name: 'Todowrite', input: { todos: [{ content: 'Second list', status: 'pending' }] } }
      }),
      event({ id: 'done-2', type: 'tool.completed', sequence: 4, payload: { toolCallId: 'todo-b', name: 'Todowrite' } })
    ])
    const wrapper = mount(ActivityTimeline, {
      props: { items },
      global: { stubs: uiStubs }
    })
    const tools = wrapper.findAll('[data-tool-kind="todo"]')
    expect(tools).toHaveLength(2)
    expect(tools[0]?.text()).toContain('First list')
    expect(tools[1]?.text()).toContain('Second list')
  })

  it('renders Decision Recorded with the question and answers on one muted line', () => {
    const items = projectRunActivity([
      event({
        id: 'question',
        type: 'question.created',
        sequence: 1,
        payload: {
          questionId: 'q1',
          prompt: 'Which marker should I write?',
          kind: 'SINGLE_CHOICE',
          options: [{ id: 'alpha', label: 'alpha' }, { id: 'beta', label: 'beta' }]
        }
      }),
      event({
        id: 'answer',
        type: 'question.answered',
        sequence: 2,
        payload: { questionId: 'q1', answer: { kind: 'SINGLE_CHOICE', optionIds: ['beta'] } }
      }),
      event({
        id: 'decision',
        type: 'decision.recorded',
        sequence: 3,
        payload: { decisionId: 'd1', questionId: 'q1', kind: 'QUESTION_ANSWER' }
      })
    ])
    const wrapper = mount(ActivityTimeline, {
      props: { items },
      global: { stubs: uiStubs }
    })
    const decision = wrapper.findAll('[data-activity-kind="event"]').find(row => row.text().includes('Decision Recorded'))
    expect(decision).toBeTruthy()
    const line = decision!.get('.flex')
    expect(line.text()).toContain('Decision Recorded')
    expect(line.text()).toContain('Which marker should I write?')
    expect(line.text()).toContain('beta')
    const detail = line.findAll('span').find(span => span.text().includes('Which marker should I write?'))
    expect(detail?.classes()).toContain('text-muted')
    expect(detail?.classes()).toContain('truncate')
    expect(detail?.classes()).not.toContain('font-mono')
    expect(detail?.element.tagName).toBe('SPAN')
  })
})