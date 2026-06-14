import { getApp, getApps, initializeApp } from 'firebase/app'
import {
  GoogleAuthProvider,
  getAuth,
  onAuthStateChanged,
  signInWithPopup,
  signOut as firebaseSignOut,
  type Auth,
  type NextOrObserver,
  type User,
} from 'firebase/auth'

import { authMode, firebaseWebConfig } from './config'

let authInstance: Auth | null = null

export function firebaseAuth(): Auth {
  if (authMode !== 'firebase') {
    throw new Error('Firebase authentication is not active')
  }
  if (authInstance) {
    return authInstance
  }
  const app = getApps().length === 0 ? initializeApp(firebaseWebConfig()) : getApp()
  authInstance = getAuth(app)
  return authInstance
}

export function googleProvider(): GoogleAuthProvider {
  return new GoogleAuthProvider()
}

export async function signInWithGoogle(): Promise<User> {
  const result = await signInWithPopup(firebaseAuth(), googleProvider())
  return result.user
}

export async function signOut(): Promise<void> {
  await firebaseSignOut(firebaseAuth())
}

export function observeAuthState(observer: NextOrObserver<User>): () => void {
  return onAuthStateChanged(firebaseAuth(), observer)
}

export async function currentIDToken(): Promise<string | null> {
  const user = firebaseAuth().currentUser
  return user ? user.getIdToken() : null
}
