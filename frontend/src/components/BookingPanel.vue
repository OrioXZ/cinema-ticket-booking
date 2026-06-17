<script setup lang="ts">
import { computed } from 'vue'

import { useBookingFlow } from '../composables/useBookingFlow'
import type { Booking, ShowtimeSummary } from '../types/cinema'
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

const showtimeId = computed(() => props.selectedId)
const selected = computed(() =>
  props.showtimes.find((item) => item.showtime.id === props.selectedId),
)
const {
  seats,
  activeLock,
  loading,
  action,
  message,
  error,
  countdownText,
  realtimeStatus,
  selectSeat,
  release,
  confirm,
} = useBookingFlow({
  showtimeId,
  onConfirmed: (booking) => emit('confirmed', booking),
  onSignedOut: (message) => emit('signedOut', message),
})

defineExpose({
  hasActiveLock: () => activeLock.value !== null || action.value === 'lock',
})
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
