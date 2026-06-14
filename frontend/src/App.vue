<script setup lang="ts">
import { onBeforeUnmount, onMounted, ref } from 'vue'

import { listShowtimes } from './api/cinema'
import { authMode } from './auth/config'
import { observeAuthState, signOut as firebaseSignOut } from './auth/firebase'
import { authSession } from './auth/session'
import AdminBookings from './components/AdminBookings.vue'
import BookingPanel from './components/BookingPanel.vue'
import LoginPanel from './components/LoginPanel.vue'
import MyBookings from './components/MyBookings.vue'
import type { Booking, ShowtimeSummary } from './types/cinema'

type Tab = 'booking' | 'mine' | 'admin'

const tab = ref<Tab>('booking')
const showtimes = ref<ShowtimeSummary[]>([])
const selectedId = ref('')
const loadingCatalog = ref(false)
const catalogError = ref('')
const flash = ref('')
const bookingsRefresh = ref(0)
const bookingPanel = ref<InstanceType<typeof BookingPanel> | null>(null)
let stopAuthObserver: (() => void) | null = null

async function loadCatalog() {
  loadingCatalog.value = true
  catalogError.value = ''
  try {
    showtimes.value = await listShowtimes()
    if (!selectedId.value && showtimes.value.length) {
      selectedId.value = showtimes.value[0].showtime.id
    }
  } catch {
    catalogError.value = 'Showtimes could not be loaded.'
  } finally {
    loadingCatalog.value = false
  }
}

function signedIn() {
  flash.value = ''
  tab.value = 'booking'
  void loadCatalog()
}

function confirmed(_booking: Booking) {
  bookingsRefresh.value++
  flash.value = 'Booking confirmed. It is now available in My Bookings.'
}

function signedOut(message = '') {
  authSession.signOut()
  tab.value = 'booking'
  flash.value = message
}

async function logout() {
  if (bookingPanel.value?.hasActiveLock()) {
    tab.value = 'booking'
    flash.value = 'Release or confirm your selected seat before signing out.'
    return
  }
  if (authMode === 'firebase') {
    await firebaseSignOut()
  }
  signedOut()
}

onMounted(() => {
  void loadCatalog()
  if (authMode === 'firebase') {
    stopAuthObserver = observeAuthState(async (user) => {
      try {
        if (!user) {
          authSession.signOut()
          authSession.setReady()
          return
        }
        const result = await user.getIdTokenResult()
        authSession.signInFirebase(
          user.uid,
          result.claims.role === 'ADMIN' ? 'ADMIN' : 'USER',
          user.displayName || '',
          user.email || '',
        )
      } catch {
        authSession.signOut()
        authSession.setReady()
        flash.value = 'Your sign-in session could not be verified. Please sign in again.'
      }
    })
  }
})
onBeforeUnmount(() => stopAuthObserver?.())
</script>

<template>
  <LoginPanel v-if="authSession.state.ready && !authSession.state.authenticated" @signed-in="signedIn" />
  <main v-else-if="!authSession.state.ready" class="centered"><p>Checking authentication...</p></main>
  <div v-else class="app-shell">
    <header class="topbar">
      <div>
        <p class="eyebrow">Cinema Ticket Booking</p>
        <strong>{{ authSession.state.displayName || authSession.state.userId }}</strong>
        <span v-if="authSession.state.email" class="muted"> / {{ authSession.state.email }}</span>
      </div>
      <div class="account-actions">
        <span class="role-badge">{{ authSession.state.role }}</span>
        <span v-if="authMode === 'development'" class="dev-badge">Local auth</span>
        <button class="text-button" @click="logout">Sign out</button>
      </div>
    </header>

    <nav class="tabs" aria-label="Application sections">
      <button :class="{ active: tab === 'booking' }" @click="tab = 'booking'">Booking</button>
      <button :class="{ active: tab === 'mine' }" @click="tab = 'mine'">My Bookings</button>
      <button v-if="authSession.isAdmin.value" :class="{ active: tab === 'admin' }" @click="tab = 'admin'">
        Admin
      </button>
    </nav>

    <p v-if="flash" class="notice success shell-notice">{{ flash }}</p>
    <p v-if="loadingCatalog" class="muted content">Loading showtimes...</p>
    <p v-else-if="catalogError" class="notice error content">{{ catalogError }}</p>
    <p v-else-if="!showtimes.length" class="muted content">No showtimes are available.</p>
    <div v-else class="content">
      <BookingPanel
        ref="bookingPanel"
        v-show="tab === 'booking'"
        :showtimes="showtimes"
        :selected-id="selectedId"
        @select-showtime="selectedId = $event"
        @confirmed="confirmed"
        @signed-out="signedOut"
      />
      <MyBookings
        v-if="tab === 'mine'"
        :key="bookingsRefresh"
        :refresh-key="bookingsRefresh"
        @signed-out="signedOut"
      />
      <AdminBookings
        v-if="tab === 'admin' && authSession.isAdmin.value"
        @signed-out="signedOut"
      />
    </div>
  </div>
</template>
