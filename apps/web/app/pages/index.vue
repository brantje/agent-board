<script setup lang="ts">
import { bootstrapCopy } from '~/utils/bootstrap'
</script>

<template>
  <UMain>
    <UContainer class="py-16 sm:py-24">
      <UCard>
        <template #header>
          <div class="flex items-center gap-3">
            <UBadge label="v0.1 bootstrap" variant="subtle" />
            <h1 class="text-2xl font-semibold text-highlighted">
              {{ bootstrapCopy.title }}
            </h1>
          </div>
        </template>

        <div class="space-y-2">
          <p class="font-medium text-highlighted">
            {{ bootstrapCopy.status }}
          </p>
          <p class="text-muted">
            {{ bootstrapCopy.description }}
          </p>
        </div>
      </UCard>
    </UContainer>
  </UMain>
</template>
