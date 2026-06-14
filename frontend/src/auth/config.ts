export type AuthMode = 'development' | 'firebase'

export type FirebaseWebConfig = {
  apiKey: string
  authDomain: string
  projectId: string
  appId: string
}

const configuredMode = import.meta.env.VITE_AUTH_MODE ?? 'development'

if (configuredMode !== 'development' && configuredMode !== 'firebase') {
  throw new Error('VITE_AUTH_MODE must be development or firebase')
}

export const authMode: AuthMode = configuredMode

export function firebaseWebConfig(): FirebaseWebConfig {
  const config = {
    apiKey: import.meta.env.VITE_FIREBASE_API_KEY?.trim() ?? '',
    authDomain: import.meta.env.VITE_FIREBASE_AUTH_DOMAIN?.trim() ?? '',
    projectId: import.meta.env.VITE_FIREBASE_PROJECT_ID?.trim() ?? '',
    appId: import.meta.env.VITE_FIREBASE_APP_ID?.trim() ?? '',
  }
  if (Object.values(config).some((value) => value === '')) {
    throw new Error('Firebase web configuration is incomplete')
  }
  return config
}

export const developmentIdentity = {
  userId: import.meta.env.VITE_DEV_USER_ID?.trim() ?? '',
  role: import.meta.env.VITE_DEV_USER_ROLE ?? 'USER',
}
