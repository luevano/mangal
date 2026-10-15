<script setup lang="ts">
import client, { ok } from '@/api/client'
import LoadState from '@/components/LoadState.vue'
import { useLoad } from '@/composables/useLoad'
import { date } from '@/composables/format'
import { crumbs } from '@/stores/crumbs'

crumbs(() => [{ title: 'Accounts' }])
const { data, loading, error } = useLoad(() => ok(client.GET('/v1/accounts')))
</script>

<template>
  <h4>Accounts</h4>
  <p class="small text-secondary">As last checked (no request here). Logins happen in the TUI or the CLI (<code>mangal accounts</code>).</p>
  <LoadState :loading="loading && !data" :error="error" :empty="data && !data.length" empty-text="No accounts." />
  <table v-if="data?.length" class="table table-sm">
    <thead><tr><th>Service</th><th>Provider</th><th>User</th><th>Status</th><th class="d-none d-md-table-cell">Expires</th><th class="d-none d-md-table-cell">Checked</th></tr></thead>
    <tbody>
      <tr v-for="a in data" :key="a.service + (a.provider ?? '')">
        <td>{{ a.service }}</td>
        <td class="small">{{ a.provider }}</td>
        <td>{{ a.user }}</td>
        <td>
          {{ a.status }}<span v-if="a.error" class="text-danger small"> · {{ a.error }}</span>
          <div v-for="d in a.details ?? []" :key="d" class="small text-secondary">{{ d }}</div>
        </td>
        <td class="d-none d-md-table-cell small">{{ date(a.expires) }}</td>
        <td class="d-none d-md-table-cell small">{{ date(a.checked) }}</td>
      </tr>
    </tbody>
  </table>
</template>
