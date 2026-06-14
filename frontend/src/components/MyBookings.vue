<script setup lang="ts">
import { onMounted, ref } from 'vue'

import { APIError } from '../api/client'
import { myBookings } from '../api/cinema'
import type { Booking } from '../types/cinema'

const props = defineProps<{ refreshKey: number }>()
const bookings = ref<Booking[]>([])
const loading = ref(false)
const error = ref('')

async function load() {
  loading.value = true
  error.value = ''
  try {
    bookings.value = await myBookings()
  } catch (value) {
    error.value =
      value instanceof APIError && value.status === 403
        ? 'Permission denied.'
        : value instanceof APIError
          ? value.message
          : 'Bookings could not be loaded.'
  } finally {
    loading.value = false
  }
}

defineExpose({ load })
onMounted(load)
</script>

<template>
  <section class="panel">
    <div class="panel-heading">
      <div>
        <p class="eyebrow">Your history</p>
        <h2>My Bookings</h2>
      </div>
      <button class="secondary small" :disabled="loading" @click="load">Refresh</button>
    </div>
    <p v-if="loading" class="muted">Loading bookings...</p>
    <p v-else-if="error" class="notice error">{{ error }}</p>
    <p v-else-if="!bookings.length" class="muted">No confirmed bookings yet.</p>
    <div v-else class="booking-list">
      <article v-for="booking in bookings" :key="booking.id" class="booking-item">
        <strong>Seat {{ booking.seat_no }}</strong>
        <span>{{ booking.showtime_id }} · {{ booking.status }}</span>
        <time>{{ new Date(booking.created_at).toLocaleString() }}</time>
        <code>{{ booking.id }}</code>
      </article>
    </div>
  </section>
</template>
