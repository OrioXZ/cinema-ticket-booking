import { computed, reactive } from 'vue'

import { authMode, developmentIdentity } from './config'
import type { Role } from '../types/cinema'

type SessionState = {
  ready: boolean
  authenticated: boolean
  userId: string
  role: Role
  displayName: string
  email: string
}

const state = reactive<SessionState>({
  ready: authMode === 'development',
  authenticated: false,
  userId: developmentIdentity.userId,
  role: 'USER',
  displayName: '',
  email: '',
})

export const authSession = {
  state,
  mode: authMode,
  isAdmin: computed(() => state.role === 'ADMIN'),
  signInDevelopment(userId: string, role: Role) {
    state.userId = userId.trim()
    state.role = role
    state.displayName = state.userId
    state.email = ''
    state.authenticated = state.userId !== ''
    state.ready = true
  },
  signInFirebase(userId: string, role: Role, displayName = '', email = '') {
    state.userId = userId
    state.role = role
    state.displayName = displayName
    state.email = email
    state.authenticated = true
    state.ready = true
  },
  signOut() {
    state.authenticated = false
    state.role = 'USER'
    state.displayName = ''
    state.email = ''
    if (authMode === 'firebase') {
      state.userId = ''
    }
  },
  setReady() {
    state.ready = true
  },
}
