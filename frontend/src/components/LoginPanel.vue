<script setup lang="ts">
import { ref } from 'vue'

import { authMode, developmentIdentity } from '../auth/config'
import { signInWithGoogle } from '../auth/firebase'
import { authSession } from '../auth/session'

const emit = defineEmits<{ signedIn: [] }>()
const userId = ref(developmentIdentity.userId || 'user-1')
const busy = ref(false)
const error = ref('')

function continueAs(role: 'USER' | 'ADMIN') {
  error.value = ''
  if (!userId.value.trim()) {
    error.value = 'Enter a development user ID.'
    return
  }
  authSession.signInDevelopment(userId.value, role)
  emit('signedIn')
}

async function googleSignIn() {
  busy.value = true
  error.value = ''
  try {
    const user = await signInWithGoogle()
    const token = await user.getIdTokenResult()
    const role = token.claims.role === 'ADMIN' ? 'ADMIN' : 'USER'
    authSession.signInFirebase(
      user.uid,
      role,
      user.displayName || '',
      user.email || '',
    )
    emit('signedIn')
  } catch {
    error.value = 'Google sign-in could not be completed.'
  } finally {
    busy.value = false
  }
}
</script>

<template>
  <main class="login-page">
    <section class="login-card">
      <p class="eyebrow">Cinema booking demo</p>
      <h1>Choose your seat</h1>
      <p class="muted">Sign in to lock a seat, confirm a mock payment, and view bookings.</p>

      <div v-if="authMode === 'development'" class="login-form">
        <span class="dev-badge">Local development authentication</span>
        <label>
          User ID
          <input v-model="userId" autocomplete="username" @keyup.enter="continueAs('USER')" />
        </label>
        <div class="button-row">
          <button class="primary" @click="continueAs('USER')">Continue as USER</button>
          <button class="secondary" @click="continueAs('ADMIN')">Continue as ADMIN</button>
        </div>
      </div>
      <button v-else class="primary wide" :disabled="busy" @click="googleSignIn">
        {{ busy ? 'Signing in...' : 'Sign in with Google' }}
      </button>
      <p v-if="error" class="notice error">{{ error }}</p>
    </section>
  </main>
</template>
