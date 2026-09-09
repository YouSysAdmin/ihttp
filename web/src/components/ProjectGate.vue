<script setup lang="ts">
// What a project-scoped page shows when no project is open: the way to
// open one, instead of a screen of failed requests.
import { useRouter } from 'vue-router'
import EmptyState from './EmptyState.vue'

const router = useRouter()
</script>

<template>
  <div class="card card-narrow">
    <EmptyState title="No project is open">
      <p>The proxy forwards traffic either way, but nothing is logged until a project is open.</p>
      <button type="button" class="btn btn-primary mt-4" @click="router.push('/projects')">
        Open a project
      </button>
    </EmptyState>
  </div>
</template>
