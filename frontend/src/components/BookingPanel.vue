<script setup lang="ts">
import { computed, onBeforeUnmount, ref, watch } from 'vue'

import { APIError, serverNow } from '../api/client'
import { confirmBooking, getSeats, lockSeat, releaseSeat } from '../api/cinema'
import { authSession } from '../auth/session'
import { useSeatRealtime } from '../composables/useSeatRealtime'
import type { Booking, Seat, SeatLock, SeatUpdate, ShowtimeSummary } from '../types/cinema'
import SeatMap from './SeatMap.vue'

const props = defineProps<{
  showtimes: ShowtimeSummary[]
  selectedId: string
}>()

const emit = defineEmits<{
  selectShowtime: [id: string]
  confirmed: [booking: Booking]
  signedOut: [message: string]
}>()

const seats = ref<Seat[]>([])
const activeLock = ref<SeatLock | null>(null)
const loading = ref(false)
const action = ref('')
const message = ref('')
const error = ref('')
const remaining = ref(0)
let refreshGeneration = 0
let countdown: number | null = null
const latestRealtime = new Map<string, { revision: number; state: Seat['state'] }>()

const showtimeId = computed(() => props.selectedId)
const selected = computed(() =>
  props.showtimes.find((item) => item.showtime.id === props.selectedId),
)
const countdownText = computed(() => {
  const minutes = Math.floor(remaining.value / 60)
  return `${String(minutes).padStart(2, '0')}:${String(remaining.value % 60).padStart(2, '0')}`
})

function handleError(value: unknown, fallback: string) {
  if (value instanceof APIError) {
    if (value.status === 401) {
      authSession.signOut()
      emit('signedOut', 'Your session ended. Please sign in again.')
      return
    }
    if (value.status === 403) error.value = 'Permission denied.'
    else if (value.status === 409) {
      error.value = 'That seat is no longer available. The seat map was refreshed.'
      void refreshSeats()
    } else error.value = value.message
  } else {
    error.value = fallback
  }
}

async function refreshSeats() {
  if (!props.selectedId) return
  const generation = ++refreshGeneration
  const requestedShowtime = props.selectedId
  loading.value = true
  try {
    const result = await getSeats(requestedShowtime)
    if (generation !== refreshGeneration || requestedShowtime !== props.selectedId) return

    seats.value = result.map((seat) => {
      const realtime = latestRealtime.get(`${requestedShowtime}:${seat.seat_no}`)
      return realtime && shouldApplyUpdate(seat, realtime)
        ? { ...seat, state: realtime.state, revision: realtime.revision }
        : seat
    })
  } catch (value) {
    if (generation === refreshGeneration && requestedShowtime === props.selectedId) {
      handleError(value, 'Seat map could not be loaded.')
    }
  } finally {
    if (generation === refreshGeneration && requestedShowtime === props.selectedId) {
      loading.value = false
    }
  }
}

function shouldApplyUpdate(
  seat: Pick<Seat, 'state' | 'revision'>,
  update: Pick<SeatUpdate, 'state' | 'revision'>,
) {
  if (seat.state === 'BOOKED' && update.state !== 'BOOKED') return false
  if (update.revision < seat.revision) return false
  if (update.revision > seat.revision) return true
  if (update.state === 'BOOKED' && seat.state !== 'BOOKED') return true
  return seat.state === 'LOCKED' && update.state === 'AVAILABLE'
}

function updateCountdown() {
  if (!activeLock.value) return
  remaining.value = Math.max(
    0,
    Math.ceil((Date.parse(activeLock.value.expires_at) - serverNow()) / 1000),
  )
  if (remaining.value === 0) {
    activeLock.value = null
    message.value = 'Your seat lock expired.'
    stopCountdown()
    void refreshSeats()
  }
}

function startCountdown() {
  stopCountdown()
  updateCountdown()
  countdown = window.setInterval(updateCountdown, 1000)
}

function stopCountdown() {
  if (countdown !== null) window.clearInterval(countdown)
  countdown = null
}

async function selectSeat(seat: Seat) {
  if (activeLock.value || action.value) return
  action.value = 'lock'
  error.value = ''
  message.value = ''
  try {
    const lock = await lockSeat(props.selectedId, seat.seat_no)
    activeLock.value = lock
    latestRealtime.set(`${lock.showtime_id}:${lock.seat_no}`, {
      revision: lock.revision,
      state: 'LOCKED',
    })
    seats.value = seats.value.map((item) =>
      item.seat_no === seat.seat_no
        ? { ...item, state: 'LOCKED', revision: lock.revision }
        : item,
    )
    startCountdown()
  } catch (value) {
    handleError(value, 'The seat could not be locked.')
  } finally {
    action.value = ''
  }
}

async function release() {
  if (!activeLock.value) return
  action.value = 'release'
  error.value = ''
  try {
    await releaseSeat(activeLock.value)
    activeLock.value = null
    stopCountdown()
    message.value = 'Seat released.'
    await refreshSeats()
  } catch (value) {
    handleError(value, 'The seat could not be released.')
  } finally {
    action.value = ''
  }
}

async function confirm() {
  if (!activeLock.value) return
  action.value = 'confirm'
  error.value = ''
  try {
    const booking = await confirmBooking(activeLock.value)
    activeLock.value = null
    stopCountdown()
    message.value = `Booking confirmed: ${booking.seat_no}.`
    await refreshSeats()
    emit('confirmed', booking)
  } catch (value) {
    handleError(value, 'The booking could not be confirmed.')
  } finally {
    action.value = ''
  }
}

function applyUpdate(update: SeatUpdate) {
  if (update.showtime_id !== props.selectedId) return

  const key = `${update.showtime_id}:${update.seat_no}`
  const previous = latestRealtime.get(key)
  if (
    previous &&
    !shouldApplyUpdate(
      { state: previous.state, revision: previous.revision },
      update,
    )
  ) return

  latestRealtime.set(key, {
    revision: update.revision,
    state: update.state,
  })
  let applied = false
  seats.value = seats.value.map((seat) => {
    if (seat.seat_no !== update.seat_no || !shouldApplyUpdate(seat, update)) return seat
    applied = true
    return { ...seat, state: update.state, revision: update.revision }
  })
  if (
    applied &&
    activeLock.value?.seat_no === update.seat_no &&
    (update.state === 'AVAILABLE' || update.state === 'BOOKED')
  ) {
    activeLock.value = null
    stopCountdown()
    message.value =
      update.state === 'BOOKED'
        ? 'The selected seat is now booked.'
        : 'The selected seat is now available again.'
  }
}

const { status: realtimeStatus } = useSeatRealtime(showtimeId, applyUpdate, refreshSeats)

defineExpose({
  hasActiveLock: () => activeLock.value !== null || action.value === 'lock',
})

watch(
  () => props.selectedId,
  () => {
    refreshGeneration++
    latestRealtime.clear()
    activeLock.value = null
    stopCountdown()
    message.value = ''
    error.value = ''
    void refreshSeats()
  },
  { immediate: true },
)
onBeforeUnmount(stopCountdown)
</script>

<template>
  <section class="panel booking-panel">
    <div class="panel-heading">
      <div>
        <p class="eyebrow">Now showing</p>
        <h2>{{ selected?.movie.title || 'Select a showtime' }}</h2>
      </div>
      <span class="connection" :class="realtimeStatus">{{ realtimeStatus }}</span>
    </div>

    <label v-if="showtimes.length > 1" class="showtime-select">
      Showtime
      <select
        :value="selectedId"
        :disabled="Boolean(activeLock) || Boolean(action)"
        @change="emit('selectShowtime', ($event.target as HTMLSelectElement).value)"
      >
        <option v-for="item in showtimes" :key="item.showtime.id" :value="item.showtime.id">
          {{ new Date(item.showtime.starts_at).toLocaleString() }}
        </option>
      </select>
    </label>

    <div v-if="selected" class="movie-meta">
      <span>{{ selected.movie.duration_minutes }} min</span>
      <span>{{ selected.showtime.auditorium }}</span>
      <span>{{ new Date(selected.showtime.starts_at).toLocaleString() }}</span>
    </div>

    <p v-if="loading && !seats.length" class="muted">Loading seat map...</p>
    <p v-else-if="!loading && !seats.length" class="muted">No seats are configured.</p>
    <SeatMap
      v-else
      :seats="seats"
      :selected-seat="activeLock?.seat_no"
      :disabled="Boolean(action)"
      @select="selectSeat"
    />

    <div v-if="activeLock" class="lock-card">
      <div>
        <span>Your seat</span>
        <strong>{{ activeLock.seat_no }}</strong>
      </div>
      <div>
        <span>Time remaining</span>
        <strong class="countdown">{{ countdownText }}</strong>
      </div>
      <div class="button-row">
        <button class="secondary" :disabled="Boolean(action)" @click="release">
          {{ action === 'release' ? 'Releasing...' : 'Release seat' }}
        </button>
        <button class="primary" :disabled="Boolean(action)" @click="confirm">
          {{ action === 'confirm' ? 'Confirming...' : 'Confirm mock payment' }}
        </button>
      </div>
    </div>
    <p v-if="message" class="notice success">{{ message }}</p>
    <p v-if="error" class="notice error">{{ error }}</p>
  </section>
</template>
