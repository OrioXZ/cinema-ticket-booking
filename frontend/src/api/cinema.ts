import { apiRequest, parseResponse } from './client'
import type { Booking, Seat, SeatLock, ShowtimeSummary } from '../types/cinema'

export async function listShowtimes(): Promise<ShowtimeSummary[]> {
  const response = await apiRequest('/showtimes')
  return (await parseResponse<{ showtimes: ShowtimeSummary[] }>(response)).showtimes
}

export async function getSeats(showtimeId: string): Promise<Seat[]> {
  const response = await apiRequest(`/showtimes/${encodeURIComponent(showtimeId)}/seats`)
  return (await parseResponse<{ seats: Seat[] }>(response)).seats
}

export async function lockSeat(showtimeId: string, seatNo: string): Promise<SeatLock> {
  return parseResponse<SeatLock>(
    await apiRequest(
      `/showtimes/${encodeURIComponent(showtimeId)}/seats/${encodeURIComponent(seatNo)}/lock`,
      { method: 'POST', authenticated: true },
    ),
  )
}

export async function releaseSeat(lock: SeatLock): Promise<void> {
  await parseResponse<void>(
    await apiRequest(
      `/showtimes/${encodeURIComponent(lock.showtime_id)}/seats/${encodeURIComponent(lock.seat_no)}/lock`,
      {
        method: 'DELETE',
        authenticated: true,
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ ownership_token: lock.ownership_token }),
      },
    ),
  )
}

export async function confirmBooking(lock: SeatLock): Promise<Booking> {
  return parseResponse<Booking>(
    await apiRequest('/bookings/confirm', {
      method: 'POST',
      authenticated: true,
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({
        showtime_id: lock.showtime_id,
        seat_no: lock.seat_no,
        ownership_token: lock.ownership_token,
      }),
    }),
  )
}

export async function myBookings(): Promise<Booking[]> {
  const response = await apiRequest('/bookings/me', { authenticated: true })
  return (await parseResponse<{ bookings: Booking[] }>(response)).bookings
}

export async function adminBookings(userId = ''): Promise<Booking[]> {
  const query = new URLSearchParams({ limit: '50' })
  if (userId.trim()) query.set('user_id', userId.trim())
  const response = await apiRequest(`/admin/bookings?${query}`, { authenticated: true })
  return (await parseResponse<{ items: Booking[] }>(response)).items
}
