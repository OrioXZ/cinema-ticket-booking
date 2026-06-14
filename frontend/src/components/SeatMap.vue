<script setup lang="ts">
import { computed } from 'vue'

import type { Seat } from '../types/cinema'

const props = defineProps<{
  seats: Seat[]
  selectedSeat?: string
  disabled?: boolean
}>()

const emit = defineEmits<{ select: [seat: Seat] }>()

const rows = computed(() => {
  const grouped = new Map<string, Seat[]>()
  for (const seat of props.seats) {
    const row = seat.seat_no.match(/^[A-Za-z]+/)?.[0] || ''
    grouped.set(row, [...(grouped.get(row) || []), seat])
  }
  for (const seats of grouped.values()) {
    seats.sort((a, b) => a.seat_no.localeCompare(b.seat_no, undefined, { numeric: true }))
  }
  return [...grouped.entries()]
})

function label(seat: Seat) {
  const owned = seat.seat_no === props.selectedSeat ? ', selected by you' : ''
  return `Seat ${seat.seat_no}, ${seat.state.toLowerCase()}${owned}`
}
</script>

<template>
  <div class="seat-map">
    <div class="screen">Screen</div>
    <div v-for="[row, rowSeats] in rows" :key="row" class="seat-row">
      <span class="row-label">{{ row }}</span>
      <button
        v-for="seat in rowSeats"
        :key="seat.seat_no"
        class="seat"
        :class="[seat.state.toLowerCase(), { selected: seat.seat_no === selectedSeat }]"
        :disabled="disabled || seat.state !== 'AVAILABLE'"
        :aria-label="label(seat)"
        @click="emit('select', seat)"
      >
        {{ seat.seat_no }}
      </button>
    </div>
    <div class="legend" aria-label="Seat legend">
      <span><i class="available"></i> Available</span>
      <span><i class="locked"></i> Locked</span>
      <span><i class="booked"></i> Booked</span>
      <span><i class="selected"></i> Your lock</span>
    </div>
  </div>
</template>
