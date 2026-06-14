import { onBeforeUnmount, ref, watch, type Ref } from 'vue'

import type { SeatUpdate } from '../types/cinema'

export type RealtimeStatus = 'connecting' | 'live' | 'reconnecting' | 'offline'

export function useSeatRealtime(
  showtimeId: Ref<string>,
  onUpdate: (update: SeatUpdate) => void,
  onConnected: () => void,
) {
  const status = ref<RealtimeStatus>('offline')
  let socket: WebSocket | null = null
  let reconnectTimer: number | null = null
  let attempts = 0
  let generation = 0
  let activeRoom = ''
  const processed = new Set<string>()
  const processedOrder: string[] = []
  const revisions = new Map<string, number>()

  function remember(eventId: string) {
    processed.add(eventId)
    processedOrder.push(eventId)
    if (processedOrder.length > 500) {
      processed.delete(processedOrder.shift()!)
    }
  }

  function close() {
    generation++
    if (reconnectTimer !== null) window.clearTimeout(reconnectTimer)
    reconnectTimer = null
    socket?.close()
    socket = null
    status.value = 'offline'
  }

  function connect() {
    const room = showtimeId.value
    if (!room) return
    if (room !== activeRoom) {
      activeRoom = room
      attempts = 0
      processed.clear()
      processedOrder.length = 0
      revisions.clear()
    }
    close()
    const currentGeneration = generation
    status.value = attempts ? 'reconnecting' : 'connecting'
    const protocol = window.location.protocol === 'https:' ? 'wss:' : 'ws:'
    socket = new WebSocket(
      `${protocol}//${window.location.host}/ws/showtimes/${encodeURIComponent(room)}`,
    )
    socket.onopen = () => {
      if (currentGeneration !== generation) return
      attempts = 0
      status.value = 'live'
      onConnected()
    }
    socket.onmessage = (event) => {
      if (currentGeneration !== generation) return
      try {
        const update = JSON.parse(event.data) as SeatUpdate
        if (
          update.type !== 'seat.updated' ||
          update.showtime_id !== room ||
          processed.has(update.event_id)
        ) return
        const seatKey = `${room}:${update.seat_no}`
        const revision = revisions.get(seatKey) ?? 0
        if (update.revision < revision) return
        remember(update.event_id)
        revisions.set(seatKey, update.revision)
        onUpdate(update)
      } catch {
        // Ignore malformed public messages.
      }
    }
    socket.onclose = () => {
      if (currentGeneration !== generation || !showtimeId.value) return
      status.value = 'reconnecting'
      attempts++
      const delay = Math.min(500 * 2 ** Math.min(attempts, 4), 5000)
      reconnectTimer = window.setTimeout(connect, delay)
    }
    socket.onerror = () => socket?.close()
  }

  watch(showtimeId, connect, { immediate: true })
  onBeforeUnmount(close)
  return { status }
}
