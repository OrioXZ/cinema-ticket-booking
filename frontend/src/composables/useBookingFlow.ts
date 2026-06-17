import { computed, onBeforeUnmount, ref, watch, type Ref } from 'vue'

import { APIError, serverNow } from '../api/client'
import { confirmBooking, getSeats, lockSeat, releaseSeat } from '../api/cinema'
import { authSession } from '../auth/session'
import type { Booking, Seat, SeatLock, SeatUpdate } from '../types/cinema'
import { useSeatRealtime } from './useSeatRealtime'

export type BookingFlowOptions = {
  showtimeId: Ref<string>
  onConfirmed: (booking: Booking) => void
  onSignedOut: (message: string) => void
}

export function useBookingFlow({
  showtimeId,
  onConfirmed,
  onSignedOut,
}: BookingFlowOptions) {
  const seats = ref<Seat[]>([])
  const activeLock = ref<SeatLock | null>(null)
  const loading = ref(false)
  const action = ref('')
  const message = ref('')
  const error = ref('')
  const remaining = ref(0)
  let refreshGeneration = 0
  let countdown: number | null = null
  // Preserve realtime updates that arrive while an authoritative REST snapshot is in flight.
  const latestRealtime = new Map<string, { revision: number; state: Seat['state'] }>()

  const countdownText = computed(() => {
    const minutes = Math.floor(remaining.value / 60)
    return `${String(minutes).padStart(2, '0')}:${String(remaining.value % 60).padStart(2, '0')}`
  })

  function handleError(value: unknown, fallback: string) {
    if (value instanceof APIError) {
      if (value.status === 401) {
        authSession.signOut()
        onSignedOut('Your session ended. Please sign in again.')
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
    if (!showtimeId.value) return
    const generation = ++refreshGeneration
    const requestedShowtime = showtimeId.value
    loading.value = true
    try {
      const result = await getSeats(requestedShowtime)
      if (generation !== refreshGeneration || requestedShowtime !== showtimeId.value) return

      seats.value = result.map((seat) => {
        const realtime = latestRealtime.get(`${requestedShowtime}:${seat.seat_no}`)
        return realtime && shouldApplyUpdate(seat, realtime)
          ? { ...seat, state: realtime.state, revision: realtime.revision }
          : seat
      })
    } catch (value) {
      if (generation === refreshGeneration && requestedShowtime === showtimeId.value) {
        handleError(value, 'Seat map could not be loaded.')
      }
    } finally {
      if (generation === refreshGeneration && requestedShowtime === showtimeId.value) {
        loading.value = false
      }
    }
  }

  function shouldApplyUpdate(
    seat: Pick<Seat, 'state' | 'revision'>,
    update: Pick<SeatUpdate, 'state' | 'revision'>,
  ) {
    // Revision precedence allows BOOKED and lock release to win ties without regressing durable bookings.
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
      const lock = await lockSeat(showtimeId.value, seat.seat_no)
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
      onConfirmed(booking)
    } catch (value) {
      handleError(value, 'The booking could not be confirmed.')
    } finally {
      action.value = ''
    }
  }

  function applyUpdate(update: SeatUpdate) {
    if (update.showtime_id !== showtimeId.value) return

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

  watch(
    showtimeId,
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

  return {
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
  }
}
