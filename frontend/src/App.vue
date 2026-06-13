<script setup lang="ts">
import { onMounted, ref } from 'vue'

type ServiceStatus = {
  status: string
}

type HealthResponse = {
  status: string
  services: Record<string, ServiceStatus>
}

const health = ref<HealthResponse | null>(null)
const error = ref('')

onMounted(async () => {
  try {
    const response = await fetch('/api/health')
    const body = (await response.json()) as HealthResponse
    health.value = body

    if (!response.ok) {
      error.value = 'One or more backend dependencies are unavailable.'
    }
  } catch {
    error.value = 'The backend health endpoint is unavailable.'
  }
})
</script>

<template>
  <main>
    <section class="card">
      <p class="eyebrow">Phase 1</p>
      <h1>Cinema Ticket Booking</h1>
      <p class="intro">
        The Vue frontend and Go API are running. Booking features arrive in later phases.
      </p>

      <div class="health">
        <h2>System health</h2>
        <p v-if="!health && !error">Checking services...</p>
        <p v-if="error" class="error">{{ error }}</p>
        <ul v-if="health">
          <li v-for="(service, name) in health.services" :key="name">
            <span>{{ name }}</span>
            <strong :class="service.status">{{ service.status }}</strong>
          </li>
        </ul>
      </div>
    </section>
  </main>
</template>

<style scoped>
main {
  display: grid;
  min-height: 100vh;
  place-items: center;
  padding: 2rem;
}

.card {
  width: min(100%, 42rem);
  padding: 2.5rem;
  border: 1px solid #29354d;
  border-radius: 1rem;
  background: #151d2d;
  box-shadow: 0 1.5rem 4rem rgb(0 0 0 / 25%);
}

.eyebrow {
  margin: 0 0 0.5rem;
  color: #8da9ff;
  font-size: 0.8rem;
  font-weight: 700;
  letter-spacing: 0.12em;
  text-transform: uppercase;
}

h1,
h2 {
  margin-top: 0;
}

h1 {
  margin-bottom: 1rem;
  font-size: clamp(2rem, 6vw, 3.5rem);
  line-height: 1;
}

.intro {
  color: #b8c1d9;
  line-height: 1.6;
}

.health {
  margin-top: 2rem;
  padding-top: 1.5rem;
  border-top: 1px solid #29354d;
}

.health h2 {
  font-size: 1rem;
}

ul {
  display: grid;
  gap: 0.75rem;
  margin: 0;
  padding: 0;
  list-style: none;
}

li {
  display: flex;
  align-items: center;
  justify-content: space-between;
  text-transform: capitalize;
}

strong {
  padding: 0.25rem 0.6rem;
  border-radius: 999px;
  font-size: 0.75rem;
  text-transform: uppercase;
}

.up {
  color: #78e0aa;
  background: #123b2c;
}

.down,
.error {
  color: #ff9b9b;
}
</style>
