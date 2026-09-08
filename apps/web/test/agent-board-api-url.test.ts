import { describe, expect, it } from 'vitest'
import { DEFAULT_AGENT_BOARD_API_URL, resolveAgentBoardApiUrl } from '../app/utils/agent-board-api-url'

describe('resolveAgentBoardApiUrl', () => {
  it('defaults to loopback development upstream', () => {
    expect(resolveAgentBoardApiUrl()).toBe(DEFAULT_AGENT_BOARD_API_URL)
    expect(resolveAgentBoardApiUrl('')).toBe(DEFAULT_AGENT_BOARD_API_URL)
  })

  it('permits loopback http upstreams', () => {
    expect(resolveAgentBoardApiUrl('http://127.0.0.1:3001')).toBe('http://127.0.0.1:3001')
    expect(resolveAgentBoardApiUrl('http://localhost:3001/')).toBe('http://localhost:3001')
    expect(resolveAgentBoardApiUrl('http://[::1]:3001')).toBe('http://[::1]:3001')
  })

  it('permits compose internal service http upstreams', () => {
    expect(resolveAgentBoardApiUrl('http://server:3001')).toBe('http://server:3001')
  })

  it('permits remote https upstreams', () => {
    expect(resolveAgentBoardApiUrl('https://agent-board.example.com')).toBe('https://agent-board.example.com')
  })

  it('rejects remote http upstreams that would forward browser credentials in cleartext', () => {
    expect(() => resolveAgentBoardApiUrl('http://agent-board.example.com')).toThrow(/HTTPS/)
    expect(() => resolveAgentBoardApiUrl('http://192.168.1.10:3001')).toThrow(/HTTPS/)
  })
})
