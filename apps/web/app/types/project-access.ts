export type ProjectRole = 'viewer' | 'member' | 'admin'

export interface ProjectRoleResponse {
  role: ProjectRole
}

export interface ProjectUserAccess {
  id: string
  username: string
  email: string
  displayName: string
  status: 'pending' | 'active' | 'disabled'
  role: ProjectRole
}

export interface ProjectGroupAccess {
  id: string
  name: string
  role: ProjectRole
}

export interface ProjectDirectoryUser {
  id: string
  username: string
  email: string
  displayName: string
}

export interface ProjectDirectoryGroup {
  id: string
  name: string
}
