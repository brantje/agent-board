export type AuthUser = {
  id: string
  username: string
  email: string
  displayName: string
  deploymentRole: 'admin' | 'member'
  status: 'pending' | 'active' | 'disabled'
  forcePasswordChange: boolean
}

export type AuthTokens = {
  accessToken: string
  accessTokenExpiresAt: string
  refreshToken: string
  refreshTokenExpiresAt: string
  user: AuthUser
}

export type StoredAuth = Pick<AuthTokens, 'accessToken' | 'accessTokenExpiresAt' | 'refreshToken' | 'refreshTokenExpiresAt'>

export type AuthSession = {
  id: string
  expiresAt: string
  createdAt: string
  lastUsedAt?: string
}

export type AuthSettings = {
  accessTokenLifetimeSeconds: number
  refreshTokenLifetimeSeconds: number
  minimumPasswordLength: number
  requireUppercase: boolean
  requireLowercase: boolean
  requireNumber: boolean
  requireSymbol: boolean
}

export type PendingUserResult = {
  user: AuthUser
  setupToken: string
  setupTokenExpiresAt: string
}

export type PasswordTokenResult = {
  token: string
  expiresAt: string
}

export type Group = {
  id: string
  name: string
  createdAt: string
  updatedAt: string
}
