<script setup lang="ts">
import { onMounted, ref } from 'vue'

import { APIError } from '../api/client'
import { adminBookings } from '../api/cinema'
import type { Booking } from '../types/cinema'

const emit = defineEmits<{
  signedOut: [message: string]
}>()
const filter = ref('')
const appliedFilter = ref('')
const items = ref<Booking[]>([])
const loading = ref(false)
const error = ref('')

async function load() {
  loading.value = true
  error.value = ''
  try {
    items.value = await adminBookings(appliedFilter.value)
  } catch (value) {
    if (value instanceof APIError && value.status === 401) {
      emit('signedOut', 'Your session ended. Please sign in again.')
      return
    }
    error.value =
      value instanceof APIError && value.status === 403
        ? 'Permission denied. The backend did not authorize this account.'
        : value instanceof APIError
          ? value.message
          : 'Admin bookings could not be loaded.'
  } finally {
    loading.value = false
  }
}

function apply() {
  appliedFilter.value = filter.value.trim()
  void load()
}

function clear() {
  filter.value = ''
  appliedFilter.value = ''
  void load()
}

onMounted(load)
</script>

<template>
  <section class="panel">
    <div class="panel-heading">
      <div>
        <p class="eyebrow">Administration</p>
        <h2>All Bookings</h2>
      </div>
    </div>
    <div class="filter-row">
      <label>
        Exact user ID
        <input v-model="filter" placeholder="user-1" @keyup.enter="apply" />
      </label>
      <button class="primary small" :disabled="loading" @click="apply">Apply</button>
      <button class="secondary small" :disabled="loading" @click="clear">Clear</button>
    </div>
    <p v-if="loading" class="muted">Loading admin bookings...</p>
    <p v-else-if="error" class="notice error">{{ error }}</p>
    <p v-else-if="!items.length" class="muted">No matching bookings.</p>
    <div v-else class="table-wrap">
      <table>
        <thead>
          <tr>
            <th>Booking</th><th>User</th><th>Showtime</th><th>Seat</th><th>Status</th><th>Created</th>
          </tr>
        </thead>
        <tbody>
          <tr v-for="booking in items" :key="booking.id">
            <td><code>{{ booking.id }}</code></td>
            <td>{{ booking.user_id }}</td>
            <td>{{ booking.showtime_id }}</td>
            <td>{{ booking.seat_no }}</td>
            <td>{{ booking.status }}</td>
            <td>{{ new Date(booking.created_at).toLocaleString() }}</td>
          </tr>
        </tbody>
      </table>
    </div>
  </section>
</template>
