<script setup lang="ts">
import { useRouter } from 'vue-router'
import EmptyState from '../components/EmptyState.vue'

const router = useRouter()
</script>

<template>
  <div class="card card-narrow">
    <EmptyState title="Page not found">
      <p>There is nothing at this address.</p>
      <button class="btn btn-primary mt-4" @click="router.push('/logs')">Go to the log</button>
    </EmptyState>
  </div>
</template>
