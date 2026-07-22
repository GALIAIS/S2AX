<template>
  <AppLayout>
    <div class="space-y-6">
      <section class="border border-gray-200 bg-white p-5 dark:border-dark-700 dark:bg-dark-800">
        <div class="flex flex-col gap-4 xl:flex-row xl:items-start xl:justify-between">
          <div class="max-w-3xl">
            <div class="flex flex-wrap items-center gap-2">
              <span class="badge badge-primary">{{ t('admin.cityVisualPacks.ledger') }}</span>
              <span class="badge badge-gray">{{ t('admin.cityVisualPacks.immutableBinding') }}</span>
            </div>
            <p class="mt-3 text-sm leading-6 text-gray-600 dark:text-dark-300">
              {{ t('admin.cityVisualPacks.description') }}
            </p>
          </div>
          <div class="flex flex-wrap items-center gap-2">
            <button type="button" class="btn btn-secondary" :disabled="loading" @click="refreshAll">
              <Icon name="refresh" size="sm" :class="loading ? 'animate-spin' : ''" />
              <span class="hidden sm:inline">{{ t('common.refresh') }}</span>
            </button>
            <button type="button" class="btn btn-primary" @click="openCreateDialog">
              <Icon name="plus" size="sm" />
              {{ t('admin.cityVisualPacks.create') }}
            </button>
          </div>
        </div>

        <div class="mt-5 grid divide-y divide-gray-200 border-y border-gray-200 text-sm dark:divide-dark-700 dark:border-dark-700 sm:grid-cols-3 sm:divide-x sm:divide-y-0">
          <div class="px-4 py-3 sm:first:pl-0">
            <p class="text-xs uppercase tracking-wide text-gray-500 dark:text-dark-400">{{ t('admin.cityVisualPacks.pack') }}</p>
            <p class="mt-1 text-2xl font-semibold tabular-nums text-gray-900 dark:text-white">{{ packs.length }}</p>
          </div>
          <div class="px-4 py-3">
            <p class="text-xs uppercase tracking-wide text-gray-500 dark:text-dark-400">{{ t('admin.cityVisualPacks.statusLabels.published') }}</p>
            <p class="mt-1 text-2xl font-semibold tabular-nums text-emerald-600 dark:text-emerald-400">{{ publishedPacks.length }}</p>
          </div>
          <div class="px-4 py-3 sm:pr-0">
            <p class="text-xs uppercase tracking-wide text-gray-500 dark:text-dark-400">{{ t('admin.cityVisualPacks.policyTitle') }}</p>
            <p class="mt-1 text-2xl font-semibold tabular-nums text-gray-900 dark:text-white">{{ policies.length }}</p>
          </div>
        </div>
      </section>

      <section class="grid gap-6 xl:grid-cols-[minmax(0,0.9fr)_minmax(0,1.1fr)]">
        <article class="border border-gray-200 bg-white p-5 dark:border-dark-700 dark:bg-dark-800">
          <div class="flex items-start gap-3">
            <div class="flex h-9 w-9 flex-shrink-0 items-center justify-center bg-primary-50 text-primary-600 dark:bg-primary-500/10 dark:text-primary-300">
              <Icon name="link" size="md" />
            </div>
            <div>
              <h2 class="font-semibold text-gray-900 dark:text-white">{{ t('admin.cityVisualPacks.policyTitle') }}</h2>
              <p class="mt-1 text-sm leading-5 text-gray-600 dark:text-dark-300">{{ t('admin.cityVisualPacks.policyDescription') }}</p>
            </div>
          </div>

          <form class="mt-5 space-y-4" @submit.prevent="handleSavePolicy">
            <div>
              <label class="input-label" for="city-visual-policy-profile">{{ t('admin.cityVisualPacks.profileID') }}</label>
              <input
                id="city-visual-policy-profile"
                v-model.trim="policyForm.spatialProfileID"
                class="input font-mono"
                maxlength="64"
                :placeholder="t('admin.cityVisualPacks.profileIDPlaceholder')"
                required
              />
              <p class="input-hint">{{ t('admin.cityVisualPacks.profileIDHint') }}</p>
            </div>
            <div>
              <label class="input-label">{{ t('admin.cityVisualPacks.targetPack') }}</label>
              <Select
                v-model="policyForm.targetPack"
                :options="publishedPackOptions"
                :placeholder="t('admin.cityVisualPacks.targetPackPlaceholder')"
                :empty-text="t('admin.cityVisualPacks.noPublishedPacks')"
                :disabled="publishedPackOptions.length === 0"
                searchable
              />
            </div>
            <button
              type="submit"
              class="btn btn-primary w-full"
              :disabled="policySaving || !policyForm.spatialProfileID || !policyForm.targetPack"
            >
              <Icon v-if="policySaving" name="refresh" size="sm" class="animate-spin" />
              <Icon v-else name="checkCircle" size="sm" />
              {{ policySaving ? t('common.saving') : t('admin.cityVisualPacks.setPolicy') }}
            </button>
          </form>
        </article>

        <article class="border border-gray-200 bg-white dark:border-dark-700 dark:bg-dark-800">
          <div class="flex items-center justify-between border-b border-gray-200 px-5 py-4 dark:border-dark-700">
            <h2 class="font-semibold text-gray-900 dark:text-white">{{ t('admin.cityVisualPacks.policyTitle') }}</h2>
            <span class="text-xs text-gray-500 dark:text-dark-400">{{ policies.length }}</span>
          </div>
          <div v-if="policiesLoading" class="flex min-h-44 items-center justify-center">
            <Icon name="refresh" size="lg" class="animate-spin text-primary-500" />
          </div>
          <div v-else-if="policies.length === 0" class="empty-state min-h-44 px-6 py-10">
            <Icon name="inbox" size="xl" class="text-gray-400 dark:text-dark-500" />
            <p class="mt-3 text-sm text-gray-500 dark:text-dark-400">{{ t('admin.cityVisualPacks.noPolicies') }}</p>
          </div>
          <div v-else class="max-h-80 overflow-auto">
            <table class="w-full min-w-[620px] text-left text-sm">
              <thead class="sticky top-0 bg-gray-50 text-xs uppercase tracking-wide text-gray-500 dark:bg-dark-800 dark:text-dark-400">
                <tr>
                  <th class="px-5 py-3">{{ t('admin.cityVisualPacks.profileID') }}</th>
                  <th class="px-5 py-3">{{ t('admin.cityVisualPacks.policyTarget') }}</th>
                  <th class="px-5 py-3">{{ t('admin.cityVisualPacks.updatedAt') }}</th>
                  <th class="px-5 py-3 text-right">{{ t('common.actions') }}</th>
                </tr>
              </thead>
              <tbody class="divide-y divide-gray-200 dark:divide-dark-700">
                <tr v-for="policy in policies" :key="`${policy.semantic_projection_version}:${policy.spatial_profile_id}`">
                  <td class="px-5 py-3">
                    <span :class="['badge', policy.spatial_profile_id === '*' ? 'badge-primary' : 'badge-gray']">
                      {{ policy.spatial_profile_id === '*' ? t('admin.cityVisualPacks.defaultPolicy') : t('admin.cityVisualPacks.exactPolicy') }}
                    </span>
                    <span class="ml-2 font-mono text-xs text-gray-700 dark:text-dark-200">{{ policy.spatial_profile_id }}</span>
                  </td>
                  <td class="px-5 py-3 font-mono text-xs text-gray-700 dark:text-dark-200">
                    {{ packLabel(policy.pack_id, policy.pack_version) }}
                  </td>
                  <td class="whitespace-nowrap px-5 py-3 text-xs text-gray-500 dark:text-dark-400">{{ formatDateTime(policy.updated_at) }}</td>
                  <td class="px-5 py-3 text-right">
                    <button type="button" class="btn btn-ghost btn-sm" @click="editPolicy(policy)">{{ t('common.edit') }}</button>
                  </td>
                </tr>
              </tbody>
            </table>
          </div>
        </article>
      </section>

      <section class="border border-gray-200 bg-white dark:border-dark-700 dark:bg-dark-800">
        <div class="flex flex-wrap items-center justify-between gap-3 border-b border-gray-200 px-5 py-4 dark:border-dark-700">
          <div>
            <h2 class="font-semibold text-gray-900 dark:text-white">{{ t('admin.cityVisualPacks.ledger') }}</h2>
            <p class="mt-1 text-sm text-gray-600 dark:text-dark-300">{{ t('admin.cityVisualPacks.safeRead') }}</p>
          </div>
          <span v-if="loading" class="inline-flex items-center gap-2 text-sm text-gray-500 dark:text-dark-400">
            <Icon name="refresh" size="sm" class="animate-spin" />
            {{ t('common.loading') }}
          </span>
        </div>

        <DataTable
          :columns="packColumns"
          :data="packs"
          :loading="loading"
          :error="loadError"
          :aria-label="t('admin.cityVisualPacks.title')"
          @retry="refreshAll"
        >
          <template #cell-pack="{ row }">
            <button type="button" class="group min-w-48 text-left" @click="openDetails(row)">
              <span class="block font-mono text-sm font-semibold text-gray-900 group-hover:text-primary-600 dark:text-gray-100 dark:group-hover:text-primary-300">
                {{ row.pack_id }}
              </span>
              <span class="mt-0.5 block font-mono text-xs text-gray-500 dark:text-dark-400">v{{ row.pack_version }}</span>
            </button>
          </template>

          <template #cell-status="{ row }">
            <span :class="['badge', statusBadgeClass(row.status)]">{{ statusLabel(row.status) }}</span>
          </template>

          <template #cell-profiles="{ row }">
            <div class="flex max-w-xs flex-wrap gap-1">
              <span v-for="profileID in compatibleProfiles(row)" :key="profileID" class="badge badge-gray font-mono text-[11px]">
                {{ profileID }}
              </span>
              <span v-if="compatibleProfiles(row).length === 0" class="text-xs text-gray-400 dark:text-dark-500">—</span>
            </div>
          </template>

          <template #cell-contract="{ row }">
            <span class="font-mono text-xs text-gray-600 dark:text-dark-300">{{ row.render_contract_version }}</span>
          </template>

          <template #cell-published_at="{ row }">
            <span class="whitespace-nowrap text-xs text-gray-500 dark:text-dark-400">
              {{ row.published_at ? formatDateTime(row.published_at) : '—' }}
            </span>
          </template>

          <template #cell-actions="{ row }">
            <RowActionMenu
              :items="packActionItems(row)"
              :aria-label="t('admin.cityVisualPacks.rowActions', { pack: packLabel(row.pack_id, row.pack_version) })"
              @select="(key) => handlePackAction(key, row)"
            />
          </template>
        </DataTable>
      </section>
    </div>

    <BaseDialog
      :show="showCreateDialog"
      :title="t('admin.cityVisualPacks.createTitle')"
      width="extra-wide"
      @close="showCreateDialog = false"
    >
      <form id="create-city-visual-pack-form" class="grid gap-4 lg:grid-cols-2" @submit.prevent="handleCreate">
        <div>
          <label class="input-label" for="city-visual-pack-id">{{ t('admin.cityVisualPacks.packID') }}</label>
          <input id="city-visual-pack-id" v-model.trim="createForm.packID" class="input font-mono" maxlength="96" required placeholder="city-pixel-japan" />
          <p class="input-hint">{{ t('admin.cityVisualPacks.packIDHint') }}</p>
        </div>
        <div>
          <label class="input-label" for="city-visual-pack-version">{{ t('admin.cityVisualPacks.packVersion') }}</label>
          <input id="city-visual-pack-version" v-model.trim="createForm.packVersion" class="input font-mono" maxlength="24" required placeholder="1.0.0" />
          <p class="input-hint">{{ t('admin.cityVisualPacks.packVersionHint') }}</p>
        </div>
        <div class="lg:col-span-2">
          <label class="input-label" for="city-visual-pack-profiles">{{ t('admin.cityVisualPacks.profileIDs') }}</label>
          <input id="city-visual-pack-profiles" v-model="createForm.profileIDs" class="input font-mono" maxlength="1024" :placeholder="t('admin.cityVisualPacks.profileIDsPlaceholder')" required />
          <p class="input-hint">{{ t('admin.cityVisualPacks.profileIDsHint') }}</p>
        </div>
        <div class="lg:col-span-2">
          <div class="mb-2 flex flex-wrap items-center justify-between gap-2">
            <label class="input-label mb-0" for="city-visual-pack-manifest">{{ t('admin.cityVisualPacks.manifest') }}</label>
            <button type="button" class="btn btn-ghost btn-sm" @click="createForm.manifest = defaultManifestText()">
              {{ t('admin.cityVisualPacks.defaultManifest') }}
            </button>
          </div>
          <textarea id="city-visual-pack-manifest" v-model="createForm.manifest" class="input min-h-80 font-mono text-xs leading-5" rows="16" spellcheck="false" required />
          <p class="input-hint">{{ t('admin.cityVisualPacks.manifestHint') }}</p>
        </div>
      </form>
      <template #footer>
        <div class="flex justify-end gap-3">
          <button type="button" class="btn btn-secondary" @click="showCreateDialog = false">{{ t('common.cancel') }}</button>
          <button type="submit" form="create-city-visual-pack-form" class="btn btn-primary" :disabled="saving">
            <Icon v-if="saving" name="refresh" size="sm" class="animate-spin" />
            {{ saving ? t('common.saving') : t('common.create') }}
          </button>
        </div>
      </template>
    </BaseDialog>

    <BaseDialog
      :show="showEditDialog"
      :title="t('admin.cityVisualPacks.editTitle')"
      width="extra-wide"
      @close="showEditDialog = false"
    >
      <form id="edit-city-visual-pack-form" class="grid gap-4 lg:grid-cols-2" @submit.prevent="handleUpdate">
        <div>
          <label class="input-label">{{ t('admin.cityVisualPacks.packID') }}</label>
          <input :value="editingPack ? editingPack.pack_id : ''" class="input font-mono opacity-60" disabled />
        </div>
        <div>
          <label class="input-label">{{ t('admin.cityVisualPacks.packVersion') }}</label>
          <input :value="editingPack ? editingPack.pack_version : ''" class="input font-mono opacity-60" disabled />
        </div>
        <div class="lg:col-span-2">
          <label class="input-label" for="city-visual-edit-profiles">{{ t('admin.cityVisualPacks.profileIDs') }}</label>
          <input id="city-visual-edit-profiles" v-model="editForm.profileIDs" class="input font-mono" maxlength="1024" :placeholder="t('admin.cityVisualPacks.profileIDsPlaceholder')" required />
          <p class="input-hint">{{ t('admin.cityVisualPacks.profileIDsHint') }}</p>
        </div>
        <div class="lg:col-span-2">
          <div class="mb-2 flex flex-wrap items-center justify-between gap-2">
            <label class="input-label mb-0" for="city-visual-edit-manifest">{{ t('admin.cityVisualPacks.manifest') }}</label>
            <button type="button" class="btn btn-ghost btn-sm" @click="editForm.manifest = defaultManifestText()">
              {{ t('admin.cityVisualPacks.defaultManifest') }}
            </button>
          </div>
          <textarea id="city-visual-edit-manifest" v-model="editForm.manifest" class="input min-h-80 font-mono text-xs leading-5" rows="16" spellcheck="false" required />
          <p class="input-hint">{{ t('admin.cityVisualPacks.manifestHint') }}</p>
        </div>
      </form>
      <template #footer>
        <div class="flex justify-end gap-3">
          <button type="button" class="btn btn-secondary" @click="showEditDialog = false">{{ t('common.cancel') }}</button>
          <button type="submit" form="edit-city-visual-pack-form" class="btn btn-primary" :disabled="saving">
            <Icon v-if="saving" name="refresh" size="sm" class="animate-spin" />
            {{ saving ? t('common.saving') : t('common.save') }}
          </button>
        </div>
      </template>
    </BaseDialog>

    <BaseDialog
      :show="showDetailsDialog"
      :title="selectedPack ? t('admin.cityVisualPacks.detailsTitle', { pack: packLabel(selectedPack.pack_id, selectedPack.pack_version) }) : t('admin.cityVisualPacks.inspect')"
      width="extra-wide"
      @close="closeDetails"
    >
      <div v-if="detailLoading" class="flex min-h-96 items-center justify-center">
        <Icon name="refresh" size="xl" class="animate-spin text-primary-500" />
      </div>
      <div v-else-if="selectedPackDetail" class="space-y-6">
        <div class="flex flex-col gap-4 border-b border-gray-200 pb-5 dark:border-dark-700 lg:flex-row lg:items-start lg:justify-between">
          <div>
            <div class="flex flex-wrap items-center gap-2">
              <span :class="['badge', statusBadgeClass(selectedPackDetail.status)]">{{ statusLabel(selectedPackDetail.status) }}</span>
              <span class="font-mono text-sm text-gray-700 dark:text-dark-200">{{ packLabel(selectedPackDetail.pack_id, selectedPackDetail.pack_version) }}</span>
            </div>
            <p class="mt-2 text-sm text-gray-600 dark:text-dark-300">{{ t('admin.cityVisualPacks.safeRead') }}</p>
          </div>
          <div class="flex flex-wrap gap-2">
            <button v-if="selectedPackDetail.status === 'staging'" type="button" class="btn btn-secondary" @click="openEditDialog(selectedPackDetail)">
              <Icon name="edit" size="sm" />
              {{ t('common.edit') }}
            </button>
            <button v-if="selectedPackDetail.status === 'staging'" type="button" class="btn btn-primary" @click="queuePackAction('publish', selectedPackDetail)">
              <Icon name="play" size="sm" />
              {{ t('admin.cityVisualPacks.publish') }}
            </button>
            <button v-if="selectedPackDetail.status === 'published'" type="button" class="btn btn-secondary text-amber-700 dark:text-amber-300" @click="queuePackAction('retire', selectedPackDetail)">
              <Icon name="clock" size="sm" />
              {{ t('admin.cityVisualPacks.retire') }}
            </button>
          </div>
        </div>

        <div class="grid gap-px overflow-hidden border border-gray-200 bg-gray-200 dark:border-dark-700 dark:bg-dark-700 sm:grid-cols-2 xl:grid-cols-4">
          <div class="bg-white p-3 dark:bg-dark-800">
            <p class="text-xs text-gray-500 dark:text-dark-400">{{ t('admin.cityVisualPacks.contract') }}</p>
            <p class="mt-1 break-all font-mono text-xs text-gray-800 dark:text-dark-100">{{ selectedPackDetail.render_contract_version }}</p>
          </div>
          <div class="bg-white p-3 dark:bg-dark-800">
            <p class="text-xs text-gray-500 dark:text-dark-400">{{ t('admin.cityVisualPacks.projection') }}</p>
            <p class="mt-1 break-all font-mono text-xs text-gray-800 dark:text-dark-100">{{ selectedPackDetail.semantic_projection_version }}</p>
          </div>
          <div class="bg-white p-3 dark:bg-dark-800">
            <p class="text-xs text-gray-500 dark:text-dark-400">{{ t('admin.cityVisualPacks.manifestHash') }}</p>
            <p class="mt-1 break-all font-mono text-xs text-gray-800 dark:text-dark-100">{{ selectedPackDetail.manifest_hash }}</p>
          </div>
          <div class="bg-white p-3 dark:bg-dark-800">
            <p class="text-xs text-gray-500 dark:text-dark-400">{{ t('admin.cityVisualPacks.assetSetHash') }}</p>
            <p class="mt-1 break-all font-mono text-xs text-gray-800 dark:text-dark-100">{{ selectedPackDetail.asset_set_hash }}</p>
          </div>
        </div>

        <section class="grid gap-6 xl:grid-cols-2">
          <div>
            <h3 class="text-sm font-semibold text-gray-900 dark:text-white">{{ t('admin.cityVisualPacks.manifest') }}</h3>
            <pre class="mt-3 max-h-96 overflow-auto border border-gray-200 bg-gray-50 p-4 text-xs leading-5 text-gray-700 dark:border-dark-700 dark:bg-dark-900 dark:text-dark-200">{{ prettyJSON(selectedPackDetail.manifest) }}</pre>
          </div>
          <div>
            <h3 class="text-sm font-semibold text-gray-900 dark:text-white">{{ t('admin.cityVisualPacks.provenance') }}</h3>
            <pre class="mt-3 max-h-96 overflow-auto border border-gray-200 bg-gray-50 p-4 text-xs leading-5 text-gray-700 dark:border-dark-700 dark:bg-dark-900 dark:text-dark-200">{{ prettyJSON(selectedPackDetail.provenance) }}</pre>
          </div>
        </section>

        <section class="border border-gray-200 dark:border-dark-700">
          <div class="flex flex-col gap-3 border-b border-gray-200 px-4 py-4 dark:border-dark-700 sm:flex-row sm:items-start sm:justify-between">
            <div>
              <h3 class="font-semibold text-gray-900 dark:text-white">{{ t('admin.cityVisualPacks.generationJobs') }}</h3>
              <p class="mt-1 max-w-3xl text-sm leading-5 text-gray-600 dark:text-dark-300">{{ t('admin.cityVisualPacks.generationJobsHint') }}</p>
            </div>
            <button v-if="selectedPackDetail.status === 'staging'" type="button" class="btn btn-secondary" @click="openGenerationDialog">
              <Icon name="plus" size="sm" />
              {{ t('admin.cityVisualPacks.requestGeneration') }}
            </button>
          </div>
          <div v-if="hasUnresolvedJobs" class="border-b border-amber-200 bg-amber-50 px-4 py-3 text-sm text-amber-800 dark:border-amber-900/60 dark:bg-amber-950/30 dark:text-amber-200">
            {{ t('admin.cityVisualPacks.unresolvedJobs') }}
          </div>
          <div v-if="jobsLoading" class="flex min-h-44 items-center justify-center">
            <Icon name="refresh" size="lg" class="animate-spin text-primary-500" />
          </div>
          <div v-else-if="generationJobs.length === 0" class="empty-state min-h-44 px-6 py-10">
            <Icon name="inbox" size="xl" class="text-gray-400 dark:text-dark-500" />
            <p class="mt-3 text-sm text-gray-500 dark:text-dark-400">{{ t('admin.cityVisualPacks.noJobs') }}</p>
          </div>
          <div v-else class="max-h-[30rem] overflow-auto">
            <table class="w-full min-w-[940px] text-left text-sm">
              <thead class="sticky top-0 bg-gray-50 text-xs uppercase tracking-wide text-gray-500 dark:bg-dark-800 dark:text-dark-400">
                <tr>
                  <th class="px-4 py-3">{{ t('admin.cityVisualPacks.assetClass') }}</th>
                  <th class="px-4 py-3">{{ t('admin.cityVisualPacks.status') }}</th>
                  <th class="px-4 py-3">{{ t('admin.cityVisualPacks.requestSpec') }}</th>
                  <th class="px-4 py-3">{{ t('admin.cityVisualPacks.reviewRecord') }}</th>
                  <th class="px-4 py-3">{{ t('admin.cityVisualPacks.createdAt') }}</th>
                  <th class="px-4 py-3 text-right">{{ t('common.actions') }}</th>
                </tr>
              </thead>
              <tbody class="divide-y divide-gray-200 dark:divide-dark-700">
                <tr v-for="job in generationJobs" :key="job.id">
                  <td class="px-4 py-3">
                    <p class="font-medium text-gray-800 dark:text-dark-100">{{ assetClassLabel(job.asset_class) }}</p>
                    <p class="mt-1 font-mono text-xs text-gray-500 dark:text-dark-400">#{{ job.id }}</p>
                  </td>
                  <td class="px-4 py-3"><span :class="['badge', statusBadgeClass(job.status)]">{{ statusLabel(job.status) }}</span></td>
                  <td class="max-w-xs px-4 py-3 font-mono text-xs text-gray-600 dark:text-dark-300">
                    <p>{{ job.request_spec.pixel_width }}×{{ job.request_spec.pixel_height }} · {{ job.request_spec.frame_count }}f</p>
                    <p class="mt-1 break-words">{{ job.request_spec.semantic_tags.join(', ') }}</p>
                  </td>
                  <td class="max-w-xs px-4 py-3 font-mono text-xs text-gray-600 dark:text-dark-300">
                    {{ job.review.decision || '—' }}<span v-if="job.review.reason_code"> · {{ job.review.reason_code }}</span>
                  </td>
                  <td class="whitespace-nowrap px-4 py-3 text-xs text-gray-500 dark:text-dark-400">{{ formatDateTime(job.created_at) }}</td>
                  <td class="px-4 py-3 text-right">
                    <div class="inline-flex gap-1">
                      <button v-if="canApproveJob(job)" type="button" class="btn btn-ghost btn-sm text-emerald-700 dark:text-emerald-300" @click="openReviewDialog(job, 'approved')">{{ t('admin.cityVisualPacks.approve') }}</button>
                      <button v-if="canApproveJob(job)" type="button" class="btn btn-ghost btn-sm text-red-600 dark:text-red-400" @click="openReviewDialog(job, 'rejected')">{{ t('admin.cityVisualPacks.reject') }}</button>
                      <button v-if="canCancelJob(job)" type="button" class="btn btn-ghost btn-sm text-amber-700 dark:text-amber-300" @click="openReviewDialog(job, 'cancelled')">{{ t('admin.cityVisualPacks.cancelJob') }}</button>
                    </div>
                  </td>
                </tr>
              </tbody>
            </table>
          </div>
        </section>

        <section class="border border-gray-200 dark:border-dark-700">
          <div class="border-b border-gray-200 px-4 py-4 dark:border-dark-700">
            <h3 class="font-semibold text-gray-900 dark:text-white">{{ t('admin.cityVisualPacks.auditEvents') }}</h3>
          </div>
          <div v-if="eventsLoading" class="flex min-h-36 items-center justify-center">
            <Icon name="refresh" size="lg" class="animate-spin text-primary-500" />
          </div>
          <div v-else-if="reviewEvents.length === 0" class="empty-state min-h-36 px-6 py-8">
            <Icon name="inbox" size="xl" class="text-gray-400 dark:text-dark-500" />
            <p class="mt-3 text-sm text-gray-500 dark:text-dark-400">{{ t('admin.cityVisualPacks.noEvents') }}</p>
          </div>
          <div v-else class="max-h-80 overflow-auto">
            <table class="w-full min-w-[760px] text-left text-sm">
              <thead class="sticky top-0 bg-gray-50 text-xs uppercase tracking-wide text-gray-500 dark:bg-dark-800 dark:text-dark-400">
                <tr>
                  <th class="px-4 py-3">{{ t('admin.cityVisualPacks.eventType') }}</th>
                  <th class="px-4 py-3">{{ t('admin.cityVisualPacks.actor') }}</th>
                  <th class="px-4 py-3">{{ t('admin.cityVisualPacks.eventMetadata') }}</th>
                  <th class="px-4 py-3">{{ t('admin.cityVisualPacks.createdAt') }}</th>
                </tr>
              </thead>
              <tbody class="divide-y divide-gray-200 dark:divide-dark-700">
                <tr v-for="event in reviewEvents" :key="event.id">
                  <td class="px-4 py-3 font-mono text-xs text-gray-700 dark:text-dark-200">{{ event.event_type }}</td>
                  <td class="px-4 py-3 text-xs text-gray-500 dark:text-dark-400">{{ event.actor_user_id ?? '—' }}</td>
                  <td class="max-w-lg px-4 py-3 font-mono text-xs text-gray-600 dark:text-dark-300">{{ compactJSON(event.metadata) }}</td>
                  <td class="whitespace-nowrap px-4 py-3 text-xs text-gray-500 dark:text-dark-400">{{ formatDateTime(event.created_at) }}</td>
                </tr>
              </tbody>
            </table>
          </div>
        </section>
      </div>
    </BaseDialog>

    <BaseDialog
      :show="showGenerationDialog"
      :title="t('admin.cityVisualPacks.generationRequest')"
      width="wide"
      @close="showGenerationDialog = false"
    >
      <form id="city-visual-generation-form" class="grid gap-4 sm:grid-cols-2" @submit.prevent="handleCreateGenerationJob">
        <div>
          <label class="input-label">{{ t('admin.cityVisualPacks.assetClass') }}</label>
          <Select v-model="generationForm.assetClass" :options="assetClassOptions" />
        </div>
        <div>
          <label class="input-label" for="city-visual-generation-frames">{{ t('admin.cityVisualPacks.frameCount') }}</label>
          <input id="city-visual-generation-frames" v-model.number="generationForm.frameCount" class="input font-mono" type="number" min="1" max="64" required />
        </div>
        <div>
          <label class="input-label" for="city-visual-generation-width">{{ t('admin.cityVisualPacks.pixelWidth') }}</label>
          <input id="city-visual-generation-width" v-model.number="generationForm.pixelWidth" class="input font-mono" type="number" min="8" max="1024" step="8" required />
        </div>
        <div>
          <label class="input-label" for="city-visual-generation-height">{{ t('admin.cityVisualPacks.pixelHeight') }}</label>
          <input id="city-visual-generation-height" v-model.number="generationForm.pixelHeight" class="input font-mono" type="number" min="8" max="1024" step="8" required />
        </div>
        <div class="sm:col-span-2">
          <label class="input-label" for="city-visual-generation-tags">{{ t('admin.cityVisualPacks.semanticTags') }}</label>
          <input id="city-visual-generation-tags" v-model="generationForm.semanticTags" class="input font-mono" maxlength="2048" :placeholder="t('admin.cityVisualPacks.semanticTagsPlaceholder')" required />
          <p class="input-hint">{{ t('admin.cityVisualPacks.semanticTagsHint') }}</p>
        </div>
      </form>
      <template #footer>
        <div class="flex justify-end gap-3">
          <button type="button" class="btn btn-secondary" @click="showGenerationDialog = false">{{ t('common.cancel') }}</button>
          <button type="submit" form="city-visual-generation-form" class="btn btn-primary" :disabled="generationSaving">
            <Icon v-if="generationSaving" name="refresh" size="sm" class="animate-spin" />
            {{ generationSaving ? t('common.submitting') : t('common.submit') }}
          </button>
        </div>
      </template>
    </BaseDialog>

    <BaseDialog
      :show="showReviewDialog"
      :title="t('admin.cityVisualPacks.review')"
      width="normal"
      @close="showReviewDialog = false"
    >
      <form id="city-visual-generation-review-form" class="space-y-4" @submit.prevent="handleReviewGenerationJob">
        <p class="text-sm text-gray-600 dark:text-dark-300">
          {{ reviewForm.job ? `${assetClassLabel(reviewForm.job.asset_class)} #${reviewForm.job.id}` : '' }}
        </p>
        <div>
          <label class="input-label" for="city-visual-review-reason">{{ t('admin.cityVisualPacks.reviewReason') }}</label>
          <input id="city-visual-review-reason" v-model.trim="reviewForm.reasonCode" class="input font-mono" maxlength="64" :placeholder="t('admin.cityVisualPacks.reviewReasonPlaceholder')" />
        </div>
      </form>
      <template #footer>
        <div class="flex justify-end gap-3">
          <button type="button" class="btn btn-secondary" @click="showReviewDialog = false">{{ t('common.cancel') }}</button>
          <button type="submit" form="city-visual-generation-review-form" class="btn btn-primary" :disabled="reviewSaving">
            <Icon v-if="reviewSaving" name="refresh" size="sm" class="animate-spin" />
            {{ reviewSaving ? t('common.saving') : t('common.confirm') }}
          </button>
        </div>
      </template>
    </BaseDialog>

    <ConfirmDialog
      :show="pendingPackAction !== null"
      :title="pendingPackAction?.kind === 'publish' ? t('admin.cityVisualPacks.publishTitle') : t('admin.cityVisualPacks.retireTitle')"
      :message="pendingPackAction?.kind === 'publish' ? t('admin.cityVisualPacks.publishConfirm') : t('admin.cityVisualPacks.retireConfirm')"
      :confirm-text="pendingPackAction?.kind === 'publish' ? t('admin.cityVisualPacks.publish') : t('admin.cityVisualPacks.retire')"
      :danger="pendingPackAction?.kind === 'retire'"
      @confirm="handlePackActionConfirm"
      @cancel="pendingPackAction = null"
    />
  </AppLayout>
</template>

<script setup lang="ts">
import { computed, onMounted, reactive, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { adminAPI } from '@/api/admin'
import type {
  CityVisualGenerationAssetClass,
  CityVisualGenerationJob,
  CityVisualGenerationJobReviewRequest,
  CityVisualPackDetail,
  CityVisualPackSummary,
  CityVisualProceduralManifest,
  CityVisualReleasePolicy,
  CityVisualReviewEvent
} from '@/api/admin/cityVisualPacks'
import { useAppStore } from '@/stores/app'
import { formatDateTime } from '@/utils/format'
import AppLayout from '@/components/layout/AppLayout.vue'
import BaseDialog from '@/components/common/BaseDialog.vue'
import ConfirmDialog from '@/components/common/ConfirmDialog.vue'
import DataTable from '@/components/common/DataTable.vue'
import RowActionMenu, { type RowActionMenuItem } from '@/components/common/RowActionMenu.vue'
import Select, { type SelectOption } from '@/components/common/Select.vue'
import Icon from '@/components/icons/Icon.vue'
import type { Column } from '@/components/common/types'

type PackActionKind = 'publish' | 'retire'

interface PackAction {
  kind: PackActionKind
  pack: CityVisualPackSummary
}

const { t } = useI18n()
const appStore = useAppStore()

const packs = ref<CityVisualPackSummary[]>([])
const policies = ref<CityVisualReleasePolicy[]>([])
const loading = ref(true)
const policiesLoading = ref(true)
const loadError = ref<string | null>(null)
const saving = ref(false)
const policySaving = ref(false)

const showCreateDialog = ref(false)
const showEditDialog = ref(false)
const showDetailsDialog = ref(false)
const showGenerationDialog = ref(false)
const showReviewDialog = ref(false)
const pendingPackAction = ref<PackAction | null>(null)
const selectedPack = ref<CityVisualPackSummary | null>(null)
const selectedPackDetail = ref<CityVisualPackDetail | null>(null)
const editingPack = ref<CityVisualPackDetail | null>(null)
const detailLoading = ref(false)
const jobsLoading = ref(false)
const eventsLoading = ref(false)
const generationSaving = ref(false)
const reviewSaving = ref(false)
const generationJobs = ref<CityVisualGenerationJob[]>([])
const reviewEvents = ref<CityVisualReviewEvent[]>([])

const createForm = reactive({
  packID: '',
  packVersion: '1.0.0',
  profileIDs: '',
  manifest: ''
})

const editForm = reactive({
  profileIDs: '',
  manifest: ''
})

const policyForm = reactive({
  spatialProfileID: '*',
  targetPack: ''
})

const generationForm = reactive({
  assetClass: 'terrain' as CityVisualGenerationAssetClass,
  semanticTags: '',
  pixelWidth: 16,
  pixelHeight: 16,
  frameCount: 1
})

const reviewForm = reactive({
  job: null as CityVisualGenerationJob | null,
  decision: 'rejected' as CityVisualGenerationJobReviewRequest['decision'],
  reasonCode: 'operator_decision'
})

const packColumns = computed<Column[]>(() => [
  { key: 'pack', label: t('admin.cityVisualPacks.pack') },
  { key: 'status', label: t('admin.cityVisualPacks.status') },
  { key: 'profiles', label: t('admin.cityVisualPacks.profiles') },
  { key: 'contract', label: t('admin.cityVisualPacks.contract') },
  { key: 'published_at', label: t('admin.cityVisualPacks.publishedAt') },
  { key: 'actions', label: t('common.actions') }
])

const publishedPacks = computed(() => packs.value.filter((pack) => pack.status === 'published'))

const publishedPackOptions = computed<SelectOption[]>(() => publishedPacks.value.map((pack) => ({
  value: packKey(pack.pack_id, pack.pack_version),
  label: `${packLabel(pack.pack_id, pack.pack_version)} · ${compatibleProfiles(pack).join(', ') || '—'}`
})))

const assetClassOptions = computed<SelectOption[]>(() => (
  [
    'terrain',
    'infrastructure',
    'building_exterior',
    'interior',
    'furniture',
    'item',
    'vehicle',
    'character_base',
    'character_wear',
    'effect',
    'marker'
  ] as CityVisualGenerationAssetClass[]
).map((value) => ({ value, label: assetClassLabel(value) })))

const hasUnresolvedJobs = computed(() => generationJobs.value.some((job) => !['rejected', 'cancelled', 'failed'].includes(job.status)))

function defaultManifestText(): string {
  return JSON.stringify({
    schema_version: 1,
    render_mode: 'procedural_pixel_v1',
    logical_tile_px: 16,
    profile_palettes: {
      default: {
        ground: '#5f8259',
        soil: '#a57a50',
        road: '#77736b',
        water: '#3b6f97',
        building_residential: '#b66f69',
        building_commercial: '#d29a55',
        building_industrial: '#8393a4',
        structure: '#343332',
        portal: '#e1bd66',
        furniture: '#aa704a',
        overlay: '#70b8aa'
      }
    },
    semantic_rules: {
      terrain: ['deep_water', 'water', 'road', 'floor', 'soil', 'sand', 'grass'],
      building_uses: ['residential', 'commercial', 'industrial'],
      layers: ['structure', 'portal', 'furniture', 'item', 'entity', 'field', 'overlay']
    },
    assets: []
  }, null, 2)
}

function packKey(packID: string, packVersion: string): string {
  return `${packID}@${packVersion}`
}

function packLabel(packID: string, packVersion: string): string {
  return `${packID}@${packVersion}`
}

function compatibleProfiles(pack: CityVisualPackSummary): string[] {
  return pack.compatibility?.spatial_profile_ids ?? []
}

function statusLabel(status: string): string {
  return t(`admin.cityVisualPacks.statusLabels.${status}`)
}

function assetClassLabel(assetClass: CityVisualGenerationAssetClass): string {
  return t(`admin.cityVisualPacks.assetClasses.${assetClass}`)
}

function statusBadgeClass(status: string): string {
  if (status === 'published' || status === 'approved') return 'badge-success'
  if (status === 'staging' || status === 'queued' || status === 'generated' || status === 'reviewing') return 'badge-warning'
  if (status === 'retired' || status === 'rejected' || status === 'cancelled' || status === 'failed') return 'badge-gray'
  return 'badge-gray'
}

function prettyJSON(value: unknown): string {
  return JSON.stringify(value, null, 2)
}

function compactJSON(value: unknown): string {
  return JSON.stringify(value)
}

function errorMessage(error: unknown, fallback: string): string {
  return error instanceof Error && error.message ? error.message : fallback
}

function profileIDsFromText(value: string): string[] {
  return Array.from(new Set(value.split(',').map((candidate) => candidate.trim()).filter(Boolean)))
}

function semanticTagsFromText(value: string): string[] {
  return Array.from(new Set(value.split(',').map((candidate) => candidate.trim()).filter(Boolean)))
}

function manifestFromText(value: string): CityVisualProceduralManifest {
  const parsed: unknown = JSON.parse(value)
  if (!parsed || typeof parsed !== 'object' || Array.isArray(parsed)) {
    throw new Error(t('admin.cityVisualPacks.manifestInvalid'))
  }
  return parsed as CityVisualProceduralManifest
}

function findPackByKey(value: string): CityVisualPackSummary | undefined {
  return publishedPacks.value.find((pack) => packKey(pack.pack_id, pack.pack_version) === value)
}

function replacePack(updated: CityVisualPackSummary): void {
  const index = packs.value.findIndex((pack) => pack.pack_id === updated.pack_id && pack.pack_version === updated.pack_version)
  if (index >= 0) {
    packs.value.splice(index, 1, updated)
  } else {
    packs.value.unshift(updated)
  }
  if (selectedPack.value?.pack_id === updated.pack_id && selectedPack.value.pack_version === updated.pack_version) {
    selectedPack.value = updated
  }
  if (selectedPackDetail.value?.pack_id === updated.pack_id && selectedPackDetail.value.pack_version === updated.pack_version) {
    selectedPackDetail.value = { ...selectedPackDetail.value, ...updated }
  }
}

const refreshAll = async (): Promise<void> => {
  loading.value = true
  policiesLoading.value = true
  loadError.value = null
  try {
    const [nextPacks, nextPolicies] = await Promise.all([
      adminAPI.cityVisualPacks.listVisualPacks(),
      adminAPI.cityVisualPacks.listVisualReleasePolicies()
    ])
    packs.value = nextPacks
    policies.value = nextPolicies
  } catch (error: unknown) {
    loadError.value = errorMessage(error, t('admin.cityVisualPacks.loadFailed'))
  } finally {
    loading.value = false
    policiesLoading.value = false
  }
}

const loadDetails = async (pack: CityVisualPackSummary): Promise<void> => {
  detailLoading.value = true
  jobsLoading.value = true
  eventsLoading.value = true
  try {
    const [detail, jobs, events] = await Promise.all([
      adminAPI.cityVisualPacks.getVisualPack(pack.pack_id, pack.pack_version),
      adminAPI.cityVisualPacks.listVisualGenerationJobs(pack.pack_id, pack.pack_version),
      adminAPI.cityVisualPacks.listVisualReviewEvents(pack.pack_id, pack.pack_version)
    ])
    selectedPackDetail.value = detail
    generationJobs.value = jobs
    reviewEvents.value = events
    replacePack(detail)
  } catch (error: unknown) {
    appStore.showError(errorMessage(error, t('admin.cityVisualPacks.loadFailed')))
  } finally {
    detailLoading.value = false
    jobsLoading.value = false
    eventsLoading.value = false
  }
}

const openDetails = (pack: CityVisualPackSummary): void => {
  selectedPack.value = pack
  selectedPackDetail.value = null
  generationJobs.value = []
  reviewEvents.value = []
  showDetailsDialog.value = true
  void loadDetails(pack)
}

const closeDetails = (): void => {
  showDetailsDialog.value = false
  selectedPack.value = null
  selectedPackDetail.value = null
  generationJobs.value = []
  reviewEvents.value = []
}

const openCreateDialog = (): void => {
  Object.assign(createForm, {
    packID: '',
    packVersion: '1.0.0',
    profileIDs: '',
    manifest: defaultManifestText()
  })
  showCreateDialog.value = true
}

const openEditDialog = (pack: CityVisualPackDetail): void => {
  editingPack.value = pack
  Object.assign(editForm, {
    profileIDs: compatibleProfiles(pack).join(', '),
    manifest: prettyJSON(pack.manifest)
  })
  showEditDialog.value = true
}

const handleCreate = async (): Promise<void> => {
  const profiles = profileIDsFromText(createForm.profileIDs)
  if (profiles.length === 0) {
    appStore.showError(t('admin.cityVisualPacks.profilesRequired'))
    return
  }
  try {
    saving.value = true
    const created = await adminAPI.cityVisualPacks.createVisualPack({
      pack_id: createForm.packID.trim(),
      pack_version: createForm.packVersion.trim(),
      spatial_profile_ids: profiles,
      manifest: manifestFromText(createForm.manifest)
    })
    replacePack(created)
    showCreateDialog.value = false
    appStore.showSuccess(t('admin.cityVisualPacks.created'))
    openDetails(created)
  } catch (error: unknown) {
    appStore.showError(errorMessage(error, t('admin.cityVisualPacks.createFailed')))
  } finally {
    saving.value = false
  }
}

const handleUpdate = async (): Promise<void> => {
  if (!editingPack.value) return
  const profiles = profileIDsFromText(editForm.profileIDs)
  if (profiles.length === 0) {
    appStore.showError(t('admin.cityVisualPacks.profilesRequired'))
    return
  }
  try {
    saving.value = true
    const updated = await adminAPI.cityVisualPacks.updateVisualPack(editingPack.value.pack_id, editingPack.value.pack_version, {
      spatial_profile_ids: profiles,
      manifest: manifestFromText(editForm.manifest)
    })
    replacePack(updated)
    editingPack.value = updated
    showEditDialog.value = false
    appStore.showSuccess(t('admin.cityVisualPacks.updated'))
    if (showDetailsDialog.value) await loadDetails(updated)
  } catch (error: unknown) {
    appStore.showError(errorMessage(error, t('admin.cityVisualPacks.updateFailed')))
  } finally {
    saving.value = false
  }
}

const editPolicy = (policy: CityVisualReleasePolicy): void => {
  policyForm.spatialProfileID = policy.spatial_profile_id
  policyForm.targetPack = packKey(policy.pack_id, policy.pack_version)
}

const handleSavePolicy = async (): Promise<void> => {
  const target = findPackByKey(policyForm.targetPack)
  if (!target) {
    appStore.showError(t('admin.cityVisualPacks.noPublishedPacks'))
    return
  }
  try {
    policySaving.value = true
    const saved = await adminAPI.cityVisualPacks.setVisualReleasePolicy(policyForm.spatialProfileID.trim(), {
      pack_id: target.pack_id,
      pack_version: target.pack_version
    })
    const index = policies.value.findIndex((policy) => (
      policy.semantic_projection_version === saved.semantic_projection_version && policy.spatial_profile_id === saved.spatial_profile_id
    ))
    if (index >= 0) policies.value.splice(index, 1, saved)
    else policies.value.push(saved)
    policies.value.sort((left, right) => left.spatial_profile_id.localeCompare(right.spatial_profile_id))
    appStore.showSuccess(t('admin.cityVisualPacks.policySaved'))
  } catch (error: unknown) {
    appStore.showError(errorMessage(error, t('admin.cityVisualPacks.policySaveFailed')))
  } finally {
    policySaving.value = false
  }
}

const packActionItems = (pack: CityVisualPackSummary): RowActionMenuItem[] => {
  const items: RowActionMenuItem[] = [
    { key: 'inspect', label: t('admin.cityVisualPacks.inspect'), icon: 'eye' }
  ]
  if (pack.status === 'staging') {
    items.push({ key: 'edit', label: t('common.edit'), icon: 'edit' })
    items.push({ key: 'publish', label: t('admin.cityVisualPacks.publish'), icon: 'play' })
  }
  if (pack.status === 'published') {
    items.push({ key: 'retire', label: t('admin.cityVisualPacks.retire'), icon: 'clock', tone: 'warning' })
  }
  return items
}

const openEditFromRow = async (pack: CityVisualPackSummary): Promise<void> => {
  try {
    const detail = await adminAPI.cityVisualPacks.getVisualPack(pack.pack_id, pack.pack_version)
    replacePack(detail)
    openEditDialog(detail)
  } catch (error: unknown) {
    appStore.showError(errorMessage(error, t('admin.cityVisualPacks.loadFailed')))
  }
}

const handlePackAction = (action: string, pack: CityVisualPackSummary): void => {
  if (action === 'inspect') {
    openDetails(pack)
    return
  }
  if (action === 'edit') {
    void openEditFromRow(pack)
    return
  }
  if (action === 'publish' || action === 'retire') {
    queuePackAction(action, pack)
  }
}

const queuePackAction = (kind: PackActionKind, pack: CityVisualPackSummary): void => {
  pendingPackAction.value = { kind, pack }
}

const handlePackActionConfirm = async (): Promise<void> => {
  const action = pendingPackAction.value
  if (!action) return
  try {
    saving.value = true
    const updated = action.kind === 'publish'
      ? await adminAPI.cityVisualPacks.publishVisualPack(action.pack.pack_id, action.pack.pack_version)
      : await adminAPI.cityVisualPacks.retireVisualPack(action.pack.pack_id, action.pack.pack_version)
    replacePack(updated)
    pendingPackAction.value = null
    appStore.showSuccess(t(action.kind === 'publish' ? 'admin.cityVisualPacks.published' : 'admin.cityVisualPacks.retired'))
    await refreshAll()
    if (showDetailsDialog.value) await loadDetails(updated)
  } catch (error: unknown) {
    const fallback = action.kind === 'publish' ? t('admin.cityVisualPacks.publishFailed') : t('admin.cityVisualPacks.retireFailed')
    appStore.showError(errorMessage(error, fallback))
  } finally {
    saving.value = false
  }
}

const openGenerationDialog = (): void => {
  Object.assign(generationForm, {
    assetClass: 'terrain' as CityVisualGenerationAssetClass,
    semanticTags: '',
    pixelWidth: 16,
    pixelHeight: 16,
    frameCount: 1
  })
  showGenerationDialog.value = true
}

const handleCreateGenerationJob = async (): Promise<void> => {
  const pack = selectedPackDetail.value
  const tags = semanticTagsFromText(generationForm.semanticTags)
  if (!pack || tags.length === 0) return
  try {
    generationSaving.value = true
    const created = await adminAPI.cityVisualPacks.createVisualGenerationJob(pack.pack_id, pack.pack_version, {
      asset_class: generationForm.assetClass,
      semantic_tags: tags,
      pixel_width: Number(generationForm.pixelWidth),
      pixel_height: Number(generationForm.pixelHeight),
      frame_count: Number(generationForm.frameCount)
    })
    generationJobs.value.unshift(created)
    showGenerationDialog.value = false
    appStore.showSuccess(t('admin.cityVisualPacks.generationQueued'))
    await loadDetails(pack)
  } catch (error: unknown) {
    appStore.showError(errorMessage(error, t('admin.cityVisualPacks.generationFailed')))
  } finally {
    generationSaving.value = false
  }
}

function canApproveJob(job: CityVisualGenerationJob): boolean {
  return job.status === 'generated' || job.status === 'reviewing'
}

function canCancelJob(job: CityVisualGenerationJob): boolean {
  return job.status === 'draft' || job.status === 'queued' || job.status === 'generated' || job.status === 'reviewing'
}

const openReviewDialog = (job: CityVisualGenerationJob, decision: CityVisualGenerationJobReviewRequest['decision']): void => {
  reviewForm.job = job
  reviewForm.decision = decision
  reviewForm.reasonCode = 'operator_decision'
  showReviewDialog.value = true
}

const handleReviewGenerationJob = async (): Promise<void> => {
  const pack = selectedPackDetail.value
  const job = reviewForm.job
  if (!pack || !job) return
  try {
    reviewSaving.value = true
    const updated = await adminAPI.cityVisualPacks.reviewVisualGenerationJob(pack.pack_id, pack.pack_version, job.id, {
      decision: reviewForm.decision,
      reason_code: reviewForm.reasonCode.trim() || undefined
    })
    const index = generationJobs.value.findIndex((item) => item.id === updated.id)
    if (index >= 0) generationJobs.value.splice(index, 1, updated)
    showReviewDialog.value = false
    appStore.showSuccess(t('admin.cityVisualPacks.reviewed'))
    await loadDetails(pack)
  } catch (error: unknown) {
    appStore.showError(errorMessage(error, t('admin.cityVisualPacks.reviewFailed')))
  } finally {
    reviewSaving.value = false
  }
}

onMounted(() => {
  void refreshAll()
})
</script>
