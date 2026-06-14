export type Role = 'USER' | 'ADMIN'
export type SeatState = 'AVAILABLE' | 'LOCKED' | 'BOOKED'

export type Movie = {
  id: string
  title: string
  duration_minutes: number
}

export type Showtime = {
  id: string
  movie_id: string
  starts_at: string
  auditorium: string
  seats: string[]
}

export type ShowtimeSummary = {
  showtime: Showtime
  movie: Movie
}

export type Seat = {
  seat_no: string
  state: SeatState
}

export type SeatLock = {
  showtime_id: string
  seat_no: string
  user_id: string
  ownership_token: string
  expires_at: string
}

export type Booking = {
  id: string
  showtime_id: string
  seat_no: string
  user_id: string
  status: string
  created_at: string
}

export type SeatUpdate = {
  type: 'seat.updated'
  event_id: string
  showtime_id: string
  seat_no: string
  state: SeatState
  revision: number
  occurred_at: string
}
