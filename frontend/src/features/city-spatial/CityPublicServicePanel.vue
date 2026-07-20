<template>
  <section class="city-service-panel" aria-labelledby="city-service-title" :aria-busy="loading">
    <header class="city-service-header">
      <div>
        <p>{{ t('citySpatial.services.eyebrow') }}</p>
        <h2 id="city-service-title">{{ t('citySpatial.services.title') }}</h2>
        <span>{{ t('citySpatial.services.description') }}</span>
      </div>
      <div class="city-service-header-actions">
        <button
          type="button"
          class="btn btn-secondary btn-sm"
          :disabled="loading"
          @click="emit('refresh')"
        >
          <Icon name="refresh" size="sm" :class="{ 'animate-spin': loading }" />
          {{ t('common.refresh') }}
        </button>
        <button
          v-if="owner && catalog?.availability === 'available'"
          type="button"
          class="btn btn-primary btn-sm"
          :disabled="Boolean(busyCommandCode)"
          @click="openRegisterFacility"
        >
          <Icon name="plus" size="sm" />
          {{ t('citySpatial.services.actions.registerFacility') }}
        </button>
      </div>
    </header>

    <div v-if="loading && catalog" class="city-service-progress" aria-hidden="true"><span /></div>

    <div v-if="availability === 'unknown' && !catalog" class="city-service-empty">
      <span aria-hidden="true">⌁</span>
      <p>{{ t('citySpatial.services.loading') }}</p>
    </div>

    <div v-else-if="availability === 'unsupported' || catalog?.availability === 'unsupported'" class="city-service-unsupported">
      <code>{{ catalog?.simulation_version ?? '—' }}</code>
      <div>
        <strong>{{ t('citySpatial.services.unsupported.title') }}</strong>
        <p>{{ t('citySpatial.services.unsupported.description', { version: catalog?.required_version ?? 'city-f8-v1' }) }}</p>
      </div>
    </div>

    <template v-else-if="catalog?.availability === 'available' && catalog.profile && catalog.overview">
      <div class="city-service-summary">
        <div>
          <span>{{ t('citySpatial.services.metrics.facilities') }}</span>
          <strong>{{ formatInteger(catalog.overview.facility_count) }}</strong>
          <small>{{ t('citySpatial.services.metrics.operational', { count: catalog.overview.operational_facility_count }) }}</small>
        </div>
        <div>
          <span>{{ t('citySpatial.services.metrics.capacity') }}</span>
          <strong>{{ formatInteger(catalog.overview.dispatch_capacity_units) }}</strong>
          <small>{{ t('citySpatial.services.metrics.capacityLines', { count: catalog.overview.active_capacity_count }) }}</small>
        </div>
        <div>
          <span>{{ t('citySpatial.services.metrics.demand') }}</span>
          <strong>{{ formatInteger(catalog.overview.requested_units_per_tick) }}</strong>
          <small>{{ t('citySpatial.services.metrics.activeDemands', { count: catalog.overview.active_demand_count }) }}</small>
        </div>
        <div>
          <span>{{ t('citySpatial.services.metrics.delivered') }}</span>
          <strong>{{ formatInteger(catalog.overview.latest_delivered_units) }}</strong>
          <small>{{ latestTickLabel }}</small>
        </div>
        <div>
          <span>{{ t('citySpatial.services.metrics.shortage') }}</span>
          <strong :data-alert="hasShortage">{{ formatInteger(catalog.overview.latest_shortage_units) }}</strong>
          <small>{{ t('citySpatial.services.metrics.requested', { value: formatInteger(catalog.overview.latest_requested_units) }) }}</small>
        </div>
        <div>
          <span>{{ t('citySpatial.services.metrics.quality') }}</span>
          <strong>{{ formatMilli(catalog.overview.latest_weighted_quality_milli) }}</strong>
          <small>{{ catalog.profile.settlement_version }}</small>
        </div>
      </div>

      <nav class="city-service-tabs" role="tablist" :aria-label="t('citySpatial.services.title')">
        <button
          v-for="tab in tabs"
          :key="tab.value"
          type="button"
          role="tab"
          :aria-selected="activeTab === tab.value"
          :class="{ active: activeTab === tab.value }"
          @click="activeTab = tab.value"
        >
          <span>{{ tab.index }}</span>
          {{ tab.label }}
          <b>{{ tab.count }}</b>
        </button>
      </nav>

      <section v-if="activeTab === 'catalog'" class="city-service-catalog" role="tabpanel">
        <div class="city-service-section-heading">
          <div>
            <strong>{{ t('citySpatial.services.catalog.services') }}</strong>
            <span>{{ t('citySpatial.services.catalog.servicesDescription') }}</span>
          </div>
          <code>{{ catalog.profile.catalog_id }}@{{ catalog.profile.catalog_version }}</code>
        </div>
        <div class="city-service-definition-grid">
          <article v-for="definition in catalog.service_definitions" :key="definition.code">
            <header>
              <code>{{ definition.code }}</code>
              <span>{{ definition.flow_kind }}</span>
            </header>
            <strong>{{ serviceName(definition.code, definition.name) }}</strong>
            <dl>
              <div><dt>{{ t('citySpatial.services.catalog.category') }}</dt><dd>{{ definition.category }}</dd></div>
              <div><dt>{{ t('citySpatial.services.catalog.unit') }}</dt><dd>{{ definition.unit_code }}</dd></div>
            </dl>
          </article>
        </div>
        <div class="city-service-section-heading city-service-type-heading">
          <div>
            <strong>{{ t('citySpatial.services.catalog.facilityTypes') }}</strong>
            <span>{{ t('citySpatial.services.catalog.facilityTypesDescription') }}</span>
          </div>
          <span>{{ t('citySpatial.services.catalog.immutable') }}</span>
        </div>
        <div class="city-service-type-list">
          <article v-for="facilityType in catalog.facility_types" :key="facilityType.code">
            <div>
              <code>{{ facilityType.code }}</code>
              <strong>{{ facilityTypeName(facilityType.code, facilityType.name) }}</strong>
            </div>
            <span>{{ t('citySpatial.services.catalog.minimumArea', { value: formatInteger(facilityType.minimum_floor_area_sqm) }) }}</span>
            <div class="city-service-chip-list">
              <b v-for="serviceCode in facilityType.allowed_service_codes" :key="serviceCode">{{ serviceName(serviceCode, serviceCode) }}</b>
            </div>
          </article>
        </div>
      </section>

      <CityPhysicalNetworkPanel
        v-else-if="activeTab === 'networks'"
        :catalog="physicalNetworkCatalog"
        :networks="physicalNetworks"
        :nodes="physicalNetworkNodes"
        :edges="physicalNetworkEdges"
        :flows="physicalNetworkFlows"
        :facts="physicalNetworkFacts"
        :diagnostics="physicalNetworkDiagnostics"
        :service-catalog="catalog"
        :facilities="facilities"
        :demands="demands"
        :availability="physicalNetworkAvailability"
        :owner="owner"
        :loading="physicalNetworkLoading"
        :busy-command-code="busyCommandCode"
        @query="emit('network-query', $event)"
        @diagnose="emit('network-diagnose', $event)"
        @command="emit('command', $event)"
      />

      <template v-else>
        <div class="city-service-filters">
          <label>
            <span>{{ t('citySpatial.services.filters.service') }}</span>
            <Select v-model="filters.service" :options="serviceFilterOptions" :searchable="false" />
          </label>
          <label v-if="activeTab !== 'settlements'">
            <span>{{ t('citySpatial.services.filters.status') }}</span>
            <Select v-model="filters.status" :options="statusFilterOptions" :searchable="false" />
          </label>
          <label v-if="activeTab === 'facilities' || activeTab === 'demands'">
            <span>{{ t('citySpatial.services.filters.district') }}</span>
            <Select v-model="filters.district" :options="districtFilterOptions" />
          </label>
          <label v-if="activeTab === 'connections'">
            <span>{{ t('citySpatial.services.filters.facility') }}</span>
            <Select v-model="filters.facility" :options="facilityFilterOptions" />
          </label>
          <label v-if="activeTab === 'connections' || activeTab === 'settlements'">
            <span>{{ t('citySpatial.services.filters.demand') }}</span>
            <Select v-model="filters.demand" :options="demandFilterOptions" />
          </label>
          <button type="button" class="btn btn-secondary btn-sm" :disabled="loading" @click="applyFilters">
            <Icon name="filter" size="sm" />
            {{ t('citySpatial.services.filters.apply') }}
          </button>
          <button v-if="owner && activeTab === 'demands'" type="button" class="btn btn-primary btn-sm" :disabled="Boolean(busyCommandCode)" @click="openDemand()">
            <Icon name="plus" size="sm" />
            {{ t('citySpatial.services.actions.configureDemand') }}
          </button>
          <button v-if="owner && activeTab === 'connections'" type="button" class="btn btn-primary btn-sm" :disabled="Boolean(busyCommandCode)" @click="openConnection()">
            <Icon name="plus" size="sm" />
            {{ t('citySpatial.services.actions.configureConnection') }}
          </button>
        </div>

        <div v-if="activeItems.length === 0" class="city-service-empty city-service-empty-list" role="tabpanel">
          <span aria-hidden="true">·</span>
          <p>{{ t(`citySpatial.services.empty.${activeTab}`) }}</p>
        </div>

        <div v-else-if="activeTab === 'facilities'" class="city-service-table-wrap" role="tabpanel">
          <table class="city-service-table city-service-facility-table">
            <thead><tr>
              <th>{{ t('citySpatial.services.columns.facility') }}</th>
              <th>{{ t('citySpatial.services.columns.location') }}</th>
              <th>{{ t('citySpatial.services.columns.status') }}</th>
              <th>{{ t('citySpatial.services.columns.capacities') }}</th>
              <th v-if="owner">{{ t('common.actions') }}</th>
            </tr></thead>
            <tbody>
              <tr v-for="item in facilities?.items ?? []" :key="item.facility.code">
                <td>
                  <strong>{{ item.facility.name }}</strong>
                  <code>{{ item.facility.code }}</code>
                  <small>{{ facilityTypeName(item.facility.facility_type_code, item.facility.facility_type_code) }}</small>
                </td>
                <td>
                  <strong>{{ item.facility.district_code }}</strong>
                  <code>{{ item.facility.building_code }}</code>
                  <small v-if="item.facility.owner_entity_code">{{ item.facility.owner_entity_code }}</small>
                </td>
                <td>
                  <span class="city-service-status" :data-status="item.facility.status">{{ t(`citySpatial.services.status.${item.facility.status}`) }}</span>
                  <small>{{ t('citySpatial.services.reliability', { value: formatMilli(item.facility.reliability_milli) }) }}</small>
                  <code>v{{ item.facility.version }}</code>
                </td>
                <td>
                  <div v-if="item.capacities.length" class="city-service-capacity-list">
                    <button
                      v-for="capacity in item.capacities"
                      :key="capacity.service_code"
                      type="button"
                      :disabled="!owner || Boolean(busyCommandCode) || item.facility.status === 'retired'"
                      @click="openCapacity(item, capacity)"
                    >
                      <span>{{ serviceName(capacity.service_code, capacity.service_code) }}</span>
                      <strong>{{ formatInteger(capacity.dispatch_capacity_units) }} / {{ formatInteger(capacity.installed_capacity_units) }}</strong>
                      <small>{{ formatMilli(capacity.availability_milli) }} · v{{ capacity.version }}</small>
                    </button>
                  </div>
                  <span v-else class="city-service-muted">{{ t('citySpatial.services.noCapacity') }}</span>
                </td>
                <td v-if="owner">
                  <div class="city-service-row-actions">
                    <button type="button" class="btn btn-secondary btn-sm" :disabled="Boolean(busyCommandCode) || item.facility.status === 'retired'" @click="openCapacity(item)">
                      {{ t('citySpatial.services.actions.capacity') }}
                    </button>
                    <button type="button" class="btn btn-secondary btn-sm" :disabled="Boolean(busyCommandCode) || item.facility.status === 'retired'" @click="openStatus(item)">
                      {{ t('citySpatial.services.actions.transition') }}
                    </button>
                  </div>
                </td>
              </tr>
            </tbody>
          </table>
        </div>

        <div v-else-if="activeTab === 'demands'" class="city-service-table-wrap" role="tabpanel">
          <table class="city-service-table">
            <thead><tr>
              <th>{{ t('citySpatial.services.columns.demand') }}</th>
              <th>{{ t('citySpatial.services.columns.subject') }}</th>
              <th>{{ t('citySpatial.services.columns.request') }}</th>
              <th>{{ t('citySpatial.services.columns.latestSettlement') }}</th>
              <th v-if="owner">{{ t('common.actions') }}</th>
            </tr></thead>
            <tbody>
              <tr v-for="item in demands?.items ?? []" :key="item.demand.code">
                <td>
                  <strong>{{ serviceName(item.demand.service_code, item.demand.service_code) }}</strong>
                  <code>{{ item.demand.code }}</code>
                  <span class="city-service-status" :data-status="item.demand.status">{{ t(`citySpatial.services.status.${item.demand.status}`) }}</span>
                </td>
                <td>
                  <strong>{{ t(`citySpatial.services.subjectKind.${item.demand.subject_kind}`) }}</strong>
                  <code>{{ item.demand.subject_code }}</code>
                  <small>{{ item.demand.district_code }}<template v-if="item.demand.building_code"> · {{ item.demand.building_code }}</template></small>
                </td>
                <td>
                  <strong>{{ formatInteger(item.demand.requested_units_per_tick) }}</strong>
                  <small>{{ t('citySpatial.services.priority', { value: item.demand.priority }) }}</small>
                  <code>v{{ item.demand.version }}</code>
                </td>
                <td>
                  <template v-if="item.latest_settlement">
                    <strong>{{ formatInteger(item.latest_settlement.delivered_units) }} / {{ formatInteger(item.latest_settlement.requested_units) }}</strong>
                    <small :data-alert="item.latest_settlement.shortage_units > 0">{{ t('citySpatial.services.shortage', { value: formatInteger(item.latest_settlement.shortage_units) }) }}</small>
                    <code>T{{ item.latest_settlement.tick }}.{{ item.latest_settlement.sequence }} · {{ formatMilli(item.latest_settlement.quality_milli) }}</code>
                  </template>
                  <span v-else class="city-service-muted">{{ t('citySpatial.services.noSettlement') }}</span>
                </td>
                <td v-if="owner">
                  <button type="button" class="btn btn-secondary btn-sm" :disabled="Boolean(busyCommandCode) || item.demand.status === 'retired'" @click="openDemand(item.demand)">
                    {{ t('common.edit') }}
                  </button>
                </td>
              </tr>
            </tbody>
          </table>
        </div>

        <div v-else-if="activeTab === 'connections'" class="city-service-table-wrap" role="tabpanel">
          <table class="city-service-table">
            <thead><tr>
              <th>{{ t('citySpatial.services.columns.connection') }}</th>
              <th>{{ t('citySpatial.services.columns.route') }}</th>
              <th>{{ t('citySpatial.services.columns.flowLimit') }}</th>
              <th>{{ t('citySpatial.services.columns.policy') }}</th>
              <th v-if="owner">{{ t('common.actions') }}</th>
            </tr></thead>
            <tbody>
              <tr v-for="connection in connections?.items ?? []" :key="connection.code">
                <td>
                  <strong>{{ serviceName(connection.service_code, connection.service_code) }}</strong>
                  <code>{{ connection.code }}</code>
                  <span class="city-service-status" :data-status="connection.status">{{ t(`citySpatial.services.status.${connection.status}`) }}</span>
                </td>
                <td class="city-service-route">
                  <code>{{ connection.facility_code }}</code>
                  <Icon name="arrowRight" size="sm" />
                  <code>{{ connection.demand_code }}</code>
                </td>
                <td>
                  <strong>{{ formatInteger(connection.max_flow_units_per_tick) }}</strong>
                  <small>{{ t('citySpatial.services.loss', { value: formatMilli(connection.loss_milli) }) }}</small>
                </td>
                <td>
                  <strong>{{ t('citySpatial.services.preference', { value: connection.preference }) }}</strong>
                  <code>v{{ connection.version }}</code>
                </td>
                <td v-if="owner">
                  <button type="button" class="btn btn-secondary btn-sm" :disabled="Boolean(busyCommandCode) || connection.status === 'retired'" @click="openConnection(connection)">
                    {{ t('common.edit') }}
                  </button>
                </td>
              </tr>
            </tbody>
          </table>
        </div>

        <div v-else class="city-service-settlement-list" role="tabpanel">
          <article v-for="item in settlements?.items ?? []" :key="`${item.settlement.tick}:${item.settlement.sequence}`">
            <header>
              <div><code>T{{ item.settlement.tick }}.{{ item.settlement.sequence }}</code><strong>{{ serviceName(item.settlement.service_code, item.settlement.service_code) }}</strong></div>
              <span :data-alert="item.settlement.shortage_units > 0">{{ formatMilli(item.settlement.quality_milli) }}</span>
            </header>
            <div class="city-service-settlement-meter">
              <span :style="{ width: `${Math.max(0, Math.min(100, item.settlement.quality_milli / 10))}%` }" />
            </div>
            <dl>
              <div><dt>{{ t('citySpatial.services.columns.demand') }}</dt><dd>{{ item.settlement.demand_code }}</dd></div>
              <div><dt>{{ t('citySpatial.services.metrics.requestedLabel') }}</dt><dd>{{ formatInteger(item.settlement.requested_units) }}</dd></div>
              <div><dt>{{ t('citySpatial.services.metrics.delivered') }}</dt><dd>{{ formatInteger(item.settlement.delivered_units) }}</dd></div>
              <div><dt>{{ t('citySpatial.services.metrics.shortage') }}</dt><dd :data-alert="item.settlement.shortage_units > 0">{{ formatInteger(item.settlement.shortage_units) }}</dd></div>
            </dl>
            <details v-if="item.allocations.length">
              <summary>{{ t('citySpatial.services.allocations', { count: item.allocations.length }) }}</summary>
              <div class="city-service-allocation-list">
                <div v-for="allocation in item.allocations" :key="allocation.allocation_index">
                  <code>#{{ allocation.allocation_index }} · {{ allocation.connection_code }}</code>
                  <span>{{ allocation.facility_code }} → {{ allocation.demand_code }}</span>
                  <strong>{{ formatInteger(allocation.delivered_units) }} / {{ formatInteger(allocation.dispatched_units) }}</strong>
                  <small>{{ t('citySpatial.services.lossUnits', { value: formatInteger(allocation.loss_units) }) }}</small>
                </div>
              </div>
            </details>
          </article>
        </div>

        <footer v-if="activeNextCursor" class="city-service-load-more">
          <span>{{ t('citySpatial.services.pagination.loaded', { count: activeItems.length }) }}</span>
          <button type="button" class="btn btn-secondary btn-sm" :disabled="loading" @click="loadMore">
            {{ loading ? t('common.loading') : t('citySpatial.services.pagination.more') }}
          </button>
        </footer>
      </template>
    </template>

    <BaseDialog
      :show="operation !== null"
      :title="operationTitle"
      width="wide"
      @close="closeOperation"
    >
      <nav class="city-service-operation-tabs" :aria-label="t('citySpatial.services.actions.operations')">
        <button v-for="item in operationOptions" :key="item.value" type="button" :class="{ active: operation === item.value }" @click="switchOperation(item.value)">
          {{ item.label }}
        </button>
      </nav>

      <form v-if="operation === 'register'" class="city-service-form" @submit.prevent="submitOperation">
        <label><span>{{ t('citySpatial.services.form.code') }}</span><input v-model.trim="registerForm.code" class="input font-mono" maxlength="96" required /></label>
        <label><span>{{ t('citySpatial.services.form.name') }}</span><input v-model.trim="registerForm.name" class="input" maxlength="96" required /></label>
        <label><span>{{ t('citySpatial.services.form.facilityType') }}</span><Select v-model="registerForm.facilityTypeCode" :options="facilityTypeOptions" :searchable="false" /></label>
        <label><span>{{ t('citySpatial.services.form.building') }}</span><Select v-model="registerForm.buildingCode" :options="eligibleBuildingOptions" /></label>
        <label><span>{{ t('citySpatial.services.form.ownerEntity') }}</span><Select v-model="registerForm.ownerEntityCode" :options="ownerEntityOptions" searchable creatable clearable /></label>
        <label><span>{{ t('citySpatial.services.form.reliability') }}</span><input v-model.number="registerForm.reliabilityMilli" class="input font-mono" type="number" min="0" max="1000" /></label>
        <p class="city-service-form-note">{{ t('citySpatial.services.form.registerNote') }}</p>
      </form>

      <form v-else-if="operation === 'capacity'" class="city-service-form" @submit.prevent="submitOperation">
        <label><span>{{ t('citySpatial.services.form.facility') }}</span><Select v-model="capacityForm.facilityCode" :options="facilityOptions" :disabled="capacityForm.lockIdentity" /></label>
        <label><span>{{ t('citySpatial.services.form.service') }}</span><Select v-model="capacityForm.serviceCode" :options="capacityServiceOptions" :searchable="false" :disabled="capacityForm.lockIdentity" /></label>
        <label><span>{{ t('citySpatial.services.form.installedCapacity') }}</span><input v-model.number="capacityForm.installedCapacityUnits" class="input font-mono" type="number" min="1" max="922337203685477" required /></label>
        <label><span>{{ t('citySpatial.services.form.availability') }}</span><input v-model.number="capacityForm.availabilityMilli" class="input font-mono" type="number" min="0" max="1000" required /></label>
        <div class="city-service-form-preview"><span>{{ t('citySpatial.services.form.effectiveCapacity') }}</span><strong>{{ formatInteger(capacityPreview) }}</strong><code>v{{ capacityForm.expectedVersion }} → v{{ capacityForm.expectedVersion + 1 }}</code></div>
        <p class="city-service-form-note">{{ t('citySpatial.services.form.casNote') }}</p>
      </form>

      <form v-else-if="operation === 'status'" class="city-service-form" @submit.prevent="submitOperation">
        <label><span>{{ t('citySpatial.services.form.facility') }}</span><Select v-model="statusForm.facilityCode" :options="facilityOptions" @change="syncStatusFacility" /></label>
        <label><span>{{ t('citySpatial.services.form.targetStatus') }}</span><Select v-model="statusForm.toStatus" :options="statusTransitionOptions" :searchable="false" /></label>
        <div class="city-service-form-preview"><span>{{ t('citySpatial.services.form.currentStatus') }}</span><strong>{{ selectedStatusFacility ? t(`citySpatial.services.status.${selectedStatusFacility.facility.status}`) : '—' }}</strong><code>v{{ statusForm.expectedVersion }} → v{{ statusForm.expectedVersion + 1 }}</code></div>
        <p class="city-service-form-note">{{ t('citySpatial.services.form.statusNote') }}</p>
      </form>

      <form v-else-if="operation === 'demand'" class="city-service-form" @submit.prevent="submitOperation">
        <label><span>{{ t('citySpatial.services.form.code') }}</span><input v-model.trim="demandForm.code" class="input font-mono" maxlength="96" :disabled="demandForm.lockIdentity" required /></label>
        <label><span>{{ t('citySpatial.services.form.service') }}</span><Select v-model="demandForm.serviceCode" :options="serviceOptions" :searchable="false" :disabled="demandForm.lockIdentity" /></label>
        <label><span>{{ t('citySpatial.services.form.subjectKind') }}</span><Select v-model="demandForm.subjectKind" :options="subjectKindOptions" :searchable="false" :disabled="demandForm.lockIdentity" @change="resetDemandSubject" /></label>
        <label><span>{{ t('citySpatial.services.form.subject') }}</span><Select v-model="demandForm.subjectCode" :options="demandSubjectOptions" :searchable="true" :creatable="!demandForm.lockIdentity" :disabled="demandForm.lockIdentity" /></label>
        <label><span>{{ t('citySpatial.services.form.requestedUnits') }}</span><input v-model.number="demandForm.requestedUnitsPerTick" class="input font-mono" type="number" min="0" max="922337203685477" required /></label>
        <label><span>{{ t('citySpatial.services.form.priority') }}</span><input v-model.number="demandForm.priority" class="input font-mono" type="number" min="0" max="1000" required /></label>
        <label><span>{{ t('citySpatial.services.form.status') }}</span><Select v-model="demandForm.status" :options="projectionStatusOptions" :searchable="false" /></label>
        <div class="city-service-form-preview"><span>{{ t('citySpatial.services.form.version') }}</span><strong>v{{ demandForm.expectedVersion }}</strong><code>→ v{{ demandForm.expectedVersion + 1 }}</code></div>
        <p class="city-service-form-note">{{ t('citySpatial.services.form.demandNote') }}</p>
      </form>

      <form v-else-if="operation === 'connection'" class="city-service-form" @submit.prevent="submitOperation">
        <label><span>{{ t('citySpatial.services.form.code') }}</span><input v-model.trim="connectionForm.code" class="input font-mono" maxlength="96" :disabled="connectionForm.lockIdentity" required /></label>
        <label><span>{{ t('citySpatial.services.form.service') }}</span><Select v-model="connectionForm.serviceCode" :options="serviceOptions" :searchable="false" :disabled="connectionForm.lockIdentity" @change="syncConnectionReferences" /></label>
        <label><span>{{ t('citySpatial.services.form.facility') }}</span><Select v-model="connectionForm.facilityCode" :options="connectionFacilityOptions" :disabled="connectionForm.lockIdentity" /></label>
        <label><span>{{ t('citySpatial.services.form.demand') }}</span><Select v-model="connectionForm.demandCode" :options="connectionDemandOptions" :disabled="connectionForm.lockIdentity" /></label>
        <label><span>{{ t('citySpatial.services.form.flowLimit') }}</span><input v-model.number="connectionForm.maxFlowUnitsPerTick" class="input font-mono" type="number" min="1" max="922337203685477" required /></label>
        <label><span>{{ t('citySpatial.services.form.loss') }}</span><input v-model.number="connectionForm.lossMilli" class="input font-mono" type="number" min="0" max="999" required /></label>
        <label><span>{{ t('citySpatial.services.form.preference') }}</span><input v-model.number="connectionForm.preference" class="input font-mono" type="number" min="0" max="1000" required /></label>
        <label><span>{{ t('citySpatial.services.form.status') }}</span><Select v-model="connectionForm.status" :options="projectionStatusOptions" :searchable="false" /></label>
        <p class="city-service-form-note">{{ t('citySpatial.services.form.connectionNote') }}</p>
      </form>

      <template #footer>
        <button type="button" class="btn btn-secondary" @click="closeOperation">{{ t('common.cancel') }}</button>
        <button type="button" class="btn btn-primary" :disabled="!canSubmitOperation || Boolean(busyCommandCode)" @click="submitOperation">
          {{ busyCommandCode ? t('citySpatial.services.actions.processing') : t('citySpatial.services.actions.confirm') }}
        </button>
      </template>
    </BaseDialog>
  </section>
</template>

<script setup lang="ts">
import { computed, reactive, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import type {
  CityEnterpriseLocationState,
  CityFacilityServiceCapacity,
  CityPhysicalNetworkCatalogView,
  CityPhysicalNetworkDiagnosticQuery,
  CityPhysicalNetworkDiagnosticsView,
  CityPhysicalNetworkEdgePage,
  CityPhysicalNetworkFactPage,
  CityPhysicalNetworkFlowPage,
  CityPhysicalNetworkListQuery,
  CityPhysicalNetworkNodePage,
  CityPhysicalNetworkPage,
  CityServiceCatalogView,
  CityServiceCommandType,
  CityServiceConnection,
  CityServiceConnectionPage,
  CityServiceDemand,
  CityServiceDemandPage,
  CityServiceFacilityPage,
  CityServiceFacilityView,
  CityServiceListQuery,
  CityServiceProjectionStatus,
  CityServiceSettlementPage,
  CityServiceSubjectKind,
  CityLandState,
  WorldActor
} from '@/api/citySpatial'
import BaseDialog from '@/components/common/BaseDialog.vue'
import Select, { type SelectOption } from '@/components/common/Select.vue'
import Icon from '@/components/icons/Icon.vue'
import CityPhysicalNetworkPanel from './CityPhysicalNetworkPanel.vue'

type ServiceTab = 'catalog' | 'facilities' | 'demands' | 'connections' | 'networks' | 'settlements'
type ServiceSection = Exclude<ServiceTab, 'catalog' | 'networks'>
type PhysicalNetworkSection = 'networks' | 'nodes' | 'edges' | 'flows' | 'facts'
type ServiceOperation = 'register' | 'capacity' | 'status' | 'demand' | 'connection'
type PublicServiceAvailability = 'unknown' | 'available' | 'unsupported'

const props = defineProps<{
  catalog: CityServiceCatalogView | null
  facilities: CityServiceFacilityPage | null
  demands: CityServiceDemandPage | null
  connections: CityServiceConnectionPage | null
  settlements: CityServiceSettlementPage | null
  physicalNetworkCatalog: CityPhysicalNetworkCatalogView | null
  physicalNetworks: CityPhysicalNetworkPage | null
  physicalNetworkNodes: CityPhysicalNetworkNodePage | null
  physicalNetworkEdges: CityPhysicalNetworkEdgePage | null
  physicalNetworkFlows: CityPhysicalNetworkFlowPage | null
  physicalNetworkFacts: CityPhysicalNetworkFactPage | null
  physicalNetworkDiagnostics: CityPhysicalNetworkDiagnosticsView | null
  physicalNetworkAvailability: PublicServiceAvailability
  physicalNetworkLoading: boolean
  availability: PublicServiceAvailability
  landState: CityLandState | null
  enterpriseState: CityEnterpriseLocationState | null
  actors: WorldActor[]
  owner: boolean
  loading: boolean
  busyCommandCode: string | null
}>()

const emit = defineEmits<{
  (event: 'refresh'): void
  (event: 'query', value: { section: ServiceSection; query: CityServiceListQuery; append: boolean }): void
  (event: 'network-query', value: { section: PhysicalNetworkSection; query: CityPhysicalNetworkListQuery; append: boolean }): void
  (event: 'network-diagnose', value: CityPhysicalNetworkDiagnosticQuery): void
  (event: 'command', value: { commandType: CityServiceCommandType; payload: Record<string, unknown>; commandCode: string }): void
}>()

const { t, locale, te } = useI18n()
const activeTab = ref<ServiceTab>('catalog')
const operation = ref<ServiceOperation | null>(null)
const filters = reactive({ service: '', status: '', district: '', facility: '', demand: '' })

const registerForm = reactive({ code: '', name: '', facilityTypeCode: '', buildingCode: '', ownerEntityCode: '', reliabilityMilli: 1000 })
const capacityForm = reactive({ facilityCode: '', serviceCode: '', installedCapacityUnits: 1, availabilityMilli: 1000, expectedVersion: 0, lockIdentity: false })
const statusForm = reactive({ facilityCode: '', toStatus: '', expectedVersion: 0 })
const demandForm = reactive({ code: '', serviceCode: '', subjectKind: 'district' as CityServiceSubjectKind, subjectCode: '', requestedUnitsPerTick: 0, priority: 500, status: 'active' as CityServiceProjectionStatus, expectedVersion: 0, lockIdentity: false })
const connectionForm = reactive({ code: '', facilityCode: '', serviceCode: '', demandCode: '', maxFlowUnitsPerTick: 1, lossMilli: 0, preference: 500, status: 'active' as CityServiceProjectionStatus, expectedVersion: 0, lockIdentity: false })

const tabs = computed(() => [
  { value: 'catalog' as const, index: '01', label: t('citySpatial.services.tabs.catalog'), count: props.catalog?.service_definitions.length ?? 0 },
  { value: 'facilities' as const, index: '02', label: t('citySpatial.services.tabs.facilities'), count: props.facilities?.items.length ?? 0 },
  { value: 'demands' as const, index: '03', label: t('citySpatial.services.tabs.demands'), count: props.demands?.items.length ?? 0 },
  { value: 'connections' as const, index: '04', label: t('citySpatial.services.tabs.connections'), count: props.connections?.items.length ?? 0 },
  { value: 'networks' as const, index: '05', label: t('citySpatial.services.tabs.networks'), count: props.physicalNetworks?.items.length ?? 0 },
  { value: 'settlements' as const, index: '06', label: t('citySpatial.services.tabs.settlements'), count: props.settlements?.items.length ?? 0 }
])
const operationOptions = computed<Array<{ value: ServiceOperation; label: string }>>(() => [
  { value: 'register', label: t('citySpatial.services.actions.registerFacility') },
  { value: 'capacity', label: t('citySpatial.services.actions.capacity') },
  { value: 'status', label: t('citySpatial.services.actions.transition') },
  { value: 'demand', label: t('citySpatial.services.actions.configureDemand') },
  { value: 'connection', label: t('citySpatial.services.actions.configureConnection') }
])
const operationTitle = computed(() => operation.value ? operationOptions.value.find(item => item.value === operation.value)?.label ?? '' : '')
const activeItems = computed(() => {
  if (activeTab.value === 'facilities') return props.facilities?.items ?? []
  if (activeTab.value === 'demands') return props.demands?.items ?? []
  if (activeTab.value === 'connections') return props.connections?.items ?? []
  if (activeTab.value === 'settlements') return props.settlements?.items ?? []
  return props.catalog?.service_definitions ?? []
})
const activeNextCursor = computed(() => {
  if (activeTab.value === 'facilities') return props.facilities?.next_code ?? null
  if (activeTab.value === 'demands') return props.demands?.next_code ?? null
  if (activeTab.value === 'connections') return props.connections?.next_code ?? null
  if (activeTab.value === 'settlements') return props.settlements?.next_cursor ?? null
  return null
})
const hasShortage = computed(() => parseInteger(props.catalog?.overview?.latest_shortage_units ?? '0') > 0n)
const latestTickLabel = computed(() => props.catalog?.overview?.latest_settlement_tick == null
  ? t('citySpatial.services.metrics.noTick')
  : `T${props.catalog.overview.latest_settlement_tick}`)

const serviceOptions = computed<SelectOption[]>(() => props.catalog?.service_definitions.map(definition => ({ value: definition.code, label: serviceName(definition.code, definition.name) })) ?? [])
const serviceFilterOptions = computed<SelectOption[]>(() => [{ value: '', label: t('common.all') }, ...serviceOptions.value])
const projectionStatusOptions = computed<SelectOption[]>(() => ['active', 'suspended', 'retired'].map(value => ({ value, label: t(`citySpatial.services.status.${value}`) })))
const statusFilterOptions = computed<SelectOption[]>(() => {
  const values = activeTab.value === 'facilities' ? ['offline', 'operational', 'degraded', 'retired'] : ['active', 'suspended', 'retired']
  return [{ value: '', label: t('common.all') }, ...values.map(value => ({ value, label: t(`citySpatial.services.status.${value}`) }))]
})
const districtCodes = computed(() => [...new Set(props.landState?.parcels.map(parcel => parcel.district_code) ?? [])].sort())
const districtFilterOptions = computed<SelectOption[]>(() => [{ value: '', label: t('common.all') }, ...districtCodes.value.map(code => ({ value: code, label: code }))])
const facilityOptions = computed<SelectOption[]>(() => props.facilities?.items.filter(item => item.facility.status !== 'retired').map(item => ({ value: item.facility.code, label: `${item.facility.name} · ${item.facility.code}` })) ?? [])
const facilityFilterOptions = computed<SelectOption[]>(() => [{ value: '', label: t('common.all') }, ...(props.facilities?.items.map(item => ({ value: item.facility.code, label: item.facility.name })) ?? [])])
const demandFilterOptions = computed<SelectOption[]>(() => [{ value: '', label: t('common.all') }, ...(props.demands?.items.map(item => ({ value: item.demand.code, label: item.demand.code })) ?? [])])
const facilityTypeOptions = computed<SelectOption[]>(() => props.catalog?.facility_types.map(item => ({ value: item.code, label: facilityTypeName(item.code, item.name) })) ?? [])
const selectedFacilityType = computed(() => props.catalog?.facility_types.find(item => item.code === registerForm.facilityTypeCode) ?? null)
const eligibleBuildingOptions = computed<SelectOption[]>(() => props.landState?.buildings.filter(building => building.status === 'active' && building.floor_area_sqm >= (selectedFacilityType.value?.minimum_floor_area_sqm ?? 0)).map(building => ({ value: building.code, label: `${building.code} · ${building.district_code} · ${formatInteger(building.floor_area_sqm)} m²` })) ?? [])
const ownerEntityOptions = computed<SelectOption[]>(() => props.enterpriseState?.firms.map(firm => ({ value: firm.entity_code, label: `${firm.entity_name} · ${firm.entity_code}` })) ?? [])
const selectedCapacityFacility = computed(() => props.facilities?.items.find(item => item.facility.code === capacityForm.facilityCode) ?? null)
const capacityServiceOptions = computed<SelectOption[]>(() => {
  const typeCode = selectedCapacityFacility.value?.facility.facility_type_code
  const allowed = props.catalog?.facility_types.find(item => item.code === typeCode)?.allowed_service_codes ?? []
  return allowed.map(code => ({ value: code, label: serviceName(code, code) }))
})
const capacityPreview = computed(() => Math.floor(Math.max(0, capacityForm.installedCapacityUnits) * Math.max(0, capacityForm.availabilityMilli) / 1000))
const selectedStatusFacility = computed(() => props.facilities?.items.find(item => item.facility.code === statusForm.facilityCode) ?? null)
const statusTransitionOptions = computed<SelectOption[]>(() => validStatusTargets(selectedStatusFacility.value?.facility.status ?? '').map(value => ({ value, label: t(`citySpatial.services.status.${value}`) })))
const subjectKindOptions = computed<SelectOption[]>(() => ['district', 'building', 'household', 'enterprise', 'actor'].map(value => ({ value, label: t(`citySpatial.services.subjectKind.${value}`) })))
const demandSubjectOptions = computed<SelectOption[]>(() => subjectOptions(demandForm.subjectKind))
const connectionFacilityOptions = computed<SelectOption[]>(() => props.facilities?.items.filter(item => item.facility.status !== 'retired' && item.capacities.some(capacity => capacity.service_code === connectionForm.serviceCode)).map(item => ({ value: item.facility.code, label: `${item.facility.name} · ${item.facility.code}` })) ?? [])
const connectionDemandOptions = computed<SelectOption[]>(() => props.demands?.items.filter(item => item.demand.status !== 'retired' && item.demand.service_code === connectionForm.serviceCode).map(item => ({ value: item.demand.code, label: `${item.demand.code} · ${item.demand.subject_code}` })) ?? [])

const canSubmitOperation = computed(() => {
  if (operation.value === 'register') return Boolean(registerForm.code && registerForm.name && registerForm.facilityTypeCode && registerForm.buildingCode && between(registerForm.reliabilityMilli, 0, 1000))
  if (operation.value === 'capacity') return Boolean(capacityForm.facilityCode && capacityForm.serviceCode && between(capacityForm.installedCapacityUnits, 1, 922337203685477) && between(capacityForm.availabilityMilli, 0, 1000))
  if (operation.value === 'status') return Boolean(statusForm.facilityCode && statusForm.toStatus && statusTransitionOptions.value.some(item => item.value === statusForm.toStatus))
  if (operation.value === 'demand') return Boolean(demandForm.code && demandForm.serviceCode && demandForm.subjectCode && between(demandForm.requestedUnitsPerTick, 0, 922337203685477) && between(demandForm.priority, 0, 1000))
  if (operation.value === 'connection') return Boolean(connectionForm.code && connectionForm.facilityCode && connectionForm.serviceCode && connectionForm.demandCode && between(connectionForm.maxFlowUnitsPerTick, 1, 922337203685477) && between(connectionForm.lossMilli, 0, 999) && between(connectionForm.preference, 0, 1000))
  return false
})

watch(() => registerForm.facilityTypeCode, () => {
  if (!eligibleBuildingOptions.value.some(option => option.value === registerForm.buildingCode)) registerForm.buildingCode = String(eligibleBuildingOptions.value[0]?.value ?? '')
  registerForm.reliabilityMilli = selectedFacilityType.value?.default_reliability_milli ?? 1000
})
watch(() => capacityForm.facilityCode, () => {
  if (!capacityServiceOptions.value.some(option => option.value === capacityForm.serviceCode)) capacityForm.serviceCode = String(capacityServiceOptions.value[0]?.value ?? '')
})

function applyFilters(): void {
  if (activeTab.value === 'catalog' || activeTab.value === 'networks') return
  emit('query', { section: activeTab.value, query: currentQuery(), append: false })
}

function loadMore(): void {
  if (activeTab.value === 'catalog' || activeTab.value === 'networks' || !activeNextCursor.value) return
  const query = currentQuery()
  if (activeTab.value === 'settlements' && typeof activeNextCursor.value === 'object') {
    query.after_tick = activeNextCursor.value.tick
    query.after_sequence = activeNextCursor.value.sequence
  } else if (typeof activeNextCursor.value === 'string') {
    query.after_code = activeNextCursor.value
  }
  emit('query', { section: activeTab.value, query, append: true })
}

function currentQuery(): CityServiceListQuery {
  const query: CityServiceListQuery = { limit: 100 }
  if (filters.service) query.service = filters.service
  if (filters.status && activeTab.value !== 'settlements') query.status = filters.status
  if (filters.district && (activeTab.value === 'facilities' || activeTab.value === 'demands')) query.district = filters.district
  if (filters.facility && activeTab.value === 'connections') query.facility = filters.facility
  if (filters.demand && (activeTab.value === 'connections' || activeTab.value === 'settlements')) query.demand = filters.demand
  return query
}

function openRegisterFacility(): void {
  registerForm.code = ''
  registerForm.name = ''
  registerForm.facilityTypeCode = String(facilityTypeOptions.value[0]?.value ?? '')
  registerForm.buildingCode = String(eligibleBuildingOptions.value[0]?.value ?? '')
  registerForm.ownerEntityCode = ''
  registerForm.reliabilityMilli = selectedFacilityType.value?.default_reliability_milli ?? 1000
  operation.value = 'register'
}

function openCapacity(item?: CityServiceFacilityView, capacity?: CityFacilityServiceCapacity): void {
  const facility = item ?? props.facilities?.items.find(candidate => candidate.facility.status !== 'retired')
  capacityForm.facilityCode = facility?.facility.code ?? ''
  capacityForm.serviceCode = capacity?.service_code ?? String(capacityServiceOptions.value[0]?.value ?? '')
  capacityForm.installedCapacityUnits = capacity?.installed_capacity_units ?? 1
  capacityForm.availabilityMilli = capacity?.availability_milli ?? 1000
  capacityForm.expectedVersion = capacity?.version ?? 0
  capacityForm.lockIdentity = Boolean(capacity)
  operation.value = 'capacity'
}

function openStatus(item?: CityServiceFacilityView): void {
  const facility = item ?? props.facilities?.items.find(candidate => candidate.facility.status !== 'retired')
  statusForm.facilityCode = facility?.facility.code ?? ''
  statusForm.expectedVersion = facility?.facility.version ?? 0
  statusForm.toStatus = validStatusTargets(facility?.facility.status ?? '')[0] ?? ''
  operation.value = 'status'
}

function openDemand(demand?: CityServiceDemand): void {
  demandForm.code = demand?.code ?? ''
  demandForm.serviceCode = demand?.service_code ?? String(serviceOptions.value[0]?.value ?? '')
  demandForm.subjectKind = demand?.subject_kind ?? 'district'
  demandForm.subjectCode = demand?.subject_code ?? String(subjectOptions(demandForm.subjectKind)[0]?.value ?? '')
  demandForm.requestedUnitsPerTick = demand?.requested_units_per_tick ?? 0
  demandForm.priority = demand?.priority ?? 500
  demandForm.status = demand?.status ?? 'active'
  demandForm.expectedVersion = demand?.version ?? 0
  demandForm.lockIdentity = Boolean(demand)
  operation.value = 'demand'
}

function openConnection(connection?: CityServiceConnection): void {
  connectionForm.code = connection?.code ?? ''
  connectionForm.serviceCode = connection?.service_code ?? String(serviceOptions.value[0]?.value ?? '')
  connectionForm.facilityCode = connection?.facility_code ?? String(connectionFacilityOptions.value[0]?.value ?? '')
  connectionForm.demandCode = connection?.demand_code ?? String(connectionDemandOptions.value[0]?.value ?? '')
  connectionForm.maxFlowUnitsPerTick = connection?.max_flow_units_per_tick ?? 1
  connectionForm.lossMilli = connection?.loss_milli ?? 0
  connectionForm.preference = connection?.preference ?? 500
  connectionForm.status = connection?.status ?? 'active'
  connectionForm.expectedVersion = connection?.version ?? 0
  connectionForm.lockIdentity = Boolean(connection)
  operation.value = 'connection'
}

function switchOperation(next: ServiceOperation): void {
  if (next === 'register') openRegisterFacility()
  else if (next === 'capacity') openCapacity()
  else if (next === 'status') openStatus()
  else if (next === 'demand') openDemand()
  else openConnection()
}

function closeOperation(): void { operation.value = null }

function submitOperation(): void {
  if (!operation.value || !canSubmitOperation.value) return
  let commandType: CityServiceCommandType
  let payload: Record<string, unknown>
  let commandCode: string
  if (operation.value === 'register') {
    commandType = 'facility.register'
    payload = { code: registerForm.code, name: registerForm.name, facility_type_code: registerForm.facilityTypeCode, building_code: registerForm.buildingCode, reliability_milli: Math.trunc(registerForm.reliabilityMilli), metadata: {} }
    if (registerForm.ownerEntityCode) payload.owner_entity_code = registerForm.ownerEntityCode
    commandCode = `facility:${registerForm.code}`
  } else if (operation.value === 'capacity') {
    commandType = 'facility.capacity.configure'
    payload = { facility_code: capacityForm.facilityCode, service_code: capacityForm.serviceCode, installed_capacity_units: Math.trunc(capacityForm.installedCapacityUnits), availability_milli: Math.trunc(capacityForm.availabilityMilli), expected_version: capacityForm.expectedVersion, metadata: {} }
    commandCode = `capacity:${capacityForm.facilityCode}:${capacityForm.serviceCode}`
  } else if (operation.value === 'status') {
    commandType = 'facility.status.transition'
    payload = { facility_code: statusForm.facilityCode, to_status: statusForm.toStatus, expected_version: statusForm.expectedVersion, metadata: {} }
    commandCode = `status:${statusForm.facilityCode}`
  } else if (operation.value === 'demand') {
    commandType = 'service.demand.configure'
    payload = { code: demandForm.code, service_code: demandForm.serviceCode, subject_kind: demandForm.subjectKind, subject_code: demandForm.subjectCode, requested_units_per_tick: Math.trunc(demandForm.requestedUnitsPerTick), priority: Math.trunc(demandForm.priority), status: demandForm.status, expected_version: demandForm.expectedVersion, metadata: {} }
    commandCode = `demand:${demandForm.code}`
  } else {
    commandType = 'service.connection.configure'
    payload = { code: connectionForm.code, facility_code: connectionForm.facilityCode, service_code: connectionForm.serviceCode, demand_code: connectionForm.demandCode, max_flow_units_per_tick: Math.trunc(connectionForm.maxFlowUnitsPerTick), loss_milli: Math.trunc(connectionForm.lossMilli), preference: Math.trunc(connectionForm.preference), status: connectionForm.status, expected_version: connectionForm.expectedVersion, metadata: {} }
    commandCode = `connection:${connectionForm.code}`
  }
  emit('command', { commandType, payload, commandCode })
  closeOperation()
}

function syncStatusFacility(): void {
  const item = selectedStatusFacility.value
  statusForm.expectedVersion = item?.facility.version ?? 0
  statusForm.toStatus = validStatusTargets(item?.facility.status ?? '')[0] ?? ''
}

function resetDemandSubject(): void { demandForm.subjectCode = String(demandSubjectOptions.value[0]?.value ?? '') }

function syncConnectionReferences(): void {
  connectionForm.facilityCode = String(connectionFacilityOptions.value[0]?.value ?? '')
  connectionForm.demandCode = String(connectionDemandOptions.value[0]?.value ?? '')
}

function subjectOptions(kind: CityServiceSubjectKind): SelectOption[] {
  if (kind === 'district') return districtCodes.value.map(code => ({ value: code, label: code }))
  if (kind === 'building') return props.landState?.buildings.filter(item => item.status === 'active').map(item => ({ value: item.code, label: `${item.code} · ${item.district_code}` })) ?? []
  if (kind === 'enterprise') return props.enterpriseState?.firms.map(item => ({ value: item.entity_code, label: `${item.entity_name} · ${item.entity_code}` })) ?? []
  if (kind === 'actor') return props.actors.filter(item => item.status === 'active').map(item => ({ value: item.code, label: `${item.name} · ${item.code}` }))
  const seen = new Set<string>()
  const options: SelectOption[] = []
  for (const allocation of props.landState?.housing_allocations ?? []) {
    const parts = allocation.cohort_key.split('/')
    const code = parts.length >= 3 ? parts[1] : ''
    if (!code || seen.has(code)) continue
    seen.add(code)
    options.push({ value: code, label: `${code} · ${allocation.district_code}` })
  }
  return options.sort((left, right) => left.label.localeCompare(right.label))
}

function validStatusTargets(status: string): string[] {
  if (status === 'offline') return ['operational', 'retired']
  if (status === 'operational') return ['degraded', 'offline', 'retired']
  if (status === 'degraded') return ['operational', 'offline', 'retired']
  return []
}

function serviceName(code: string, fallback: string): string {
  const key = `citySpatial.services.serviceNames.${code}`
  return te(key) ? t(key) : fallback
}

function facilityTypeName(code: string, fallback: string): string {
  const key = `citySpatial.services.facilityTypeNames.${code}`
  return te(key) ? t(key) : fallback
}

function formatInteger(value: string | number | bigint): string {
  return new Intl.NumberFormat(locale.value, { maximumFractionDigits: 0 }).format(parseInteger(value))
}

function parseInteger(value: string | number | bigint): bigint {
  try { return BigInt(typeof value === 'number' ? Math.trunc(value) : value) } catch { return 0n }
}

function formatMilli(value: number): string {
  return new Intl.NumberFormat(locale.value, { style: 'percent', maximumFractionDigits: 1 }).format(value / 1000)
}

function between(value: number, minimum: number, maximum: number): boolean {
  return Number.isSafeInteger(value) && value >= minimum && value <= maximum
}
</script>

<style scoped>
.city-service-panel { position: relative; margin-top: 1rem; border: 1px solid var(--ui-separator); background: var(--ui-surface); }
.city-service-header { display: flex; align-items: center; justify-content: space-between; gap: 1rem; border-bottom: 1px solid var(--ui-separator); padding: 1rem; }
.city-service-header p { margin: 0; color: var(--ui-accent); font: 0.62rem ui-monospace, monospace; letter-spacing: 0.12em; text-transform: uppercase; }
.city-service-header h2 { margin: 0.2rem 0 0.15rem; font-size: 1rem; }
.city-service-header > div > span { color: var(--ui-label-secondary); font-size: 0.75rem; }
.city-service-header-actions, .city-service-row-actions { display: flex; flex-wrap: wrap; gap: 0.4rem; }
.city-service-progress { position: absolute; z-index: 1; top: 0; right: 0; left: 0; height: 2px; overflow: hidden; background: color-mix(in srgb, var(--ui-accent) 12%, transparent); }
.city-service-progress span { display: block; width: 32%; height: 100%; background: var(--ui-accent); animation: service-progress 1.1s ease-in-out infinite; }
@keyframes service-progress { from { transform: translateX(-110%); } to { transform: translateX(420%); } }
.city-service-summary { display: grid; grid-template-columns: repeat(6, minmax(8rem, 1fr)); border-bottom: 1px solid var(--ui-separator); }
.city-service-summary > div { min-width: 0; border-right: 1px solid var(--ui-separator); padding: 0.7rem 0.85rem; }
.city-service-summary > div:last-child { border-right: 0; }
.city-service-summary span { display: block; color: var(--ui-label-secondary); font: 0.58rem ui-monospace, monospace; letter-spacing: 0.08em; text-transform: uppercase; }
.city-service-summary strong { display: block; overflow: hidden; margin-top: 0.15rem; font: 1rem ui-monospace, monospace; text-overflow: ellipsis; }
.city-service-summary small { display: block; margin-top: 0.18rem; color: var(--ui-label-secondary); font-size: 0.58rem; }
[data-alert='true'] { color: #dc6b5d !important; }
.city-service-tabs { display: flex; overflow-x: auto; border-bottom: 1px solid var(--ui-separator); padding: 0 1rem; }
.city-service-tabs button { display: flex; min-height: 2.75rem; flex: none; align-items: center; gap: 0.45rem; border-bottom: 2px solid transparent; padding: 0 0.75rem; color: var(--ui-label-secondary); font-size: 0.7rem; }
.city-service-tabs button.active { border-bottom-color: var(--ui-accent); color: var(--ui-label); background: var(--ui-control); }
.city-service-tabs button > span { color: var(--ui-accent); font: 0.55rem ui-monospace, monospace; }
.city-service-tabs button > b { min-width: 1.2rem; padding: 0.08rem 0.25rem; background: var(--ui-control); font: 0.56rem ui-monospace, monospace; text-align: center; }
.city-service-catalog { padding-bottom: 1rem; }
.city-service-section-heading { display: flex; align-items: center; justify-content: space-between; gap: 1rem; border-bottom: 1px solid var(--ui-separator); padding: 0.75rem 1rem; }
.city-service-section-heading strong, .city-service-section-heading span { display: block; }
.city-service-section-heading strong { font-size: 0.75rem; }
.city-service-section-heading span, .city-service-section-heading code { margin-top: 0.15rem; color: var(--ui-label-secondary); font-size: 0.62rem; }
.city-service-definition-grid { display: grid; grid-template-columns: repeat(auto-fit, minmax(15rem, 1fr)); gap: 1px; background: var(--ui-separator); }
.city-service-definition-grid article { min-width: 0; padding: 0.8rem; background: var(--ui-surface); }
.city-service-definition-grid header { display: flex; justify-content: space-between; gap: 0.5rem; color: var(--ui-label-secondary); font: 0.58rem ui-monospace, monospace; }
.city-service-definition-grid > article > strong { display: block; margin-top: 0.4rem; font-size: 0.78rem; }
.city-service-definition-grid dl { display: grid; grid-template-columns: 1fr 1fr; margin: 0.65rem 0 0; border-top: 1px solid var(--ui-separator); }
.city-service-definition-grid dl div { padding-top: 0.45rem; }
.city-service-definition-grid dt { color: var(--ui-label-secondary); font-size: 0.56rem; }
.city-service-definition-grid dd { margin: 0.12rem 0 0; font: 0.62rem ui-monospace, monospace; }
.city-service-type-heading { border-top: 1px solid var(--ui-separator); }
.city-service-type-list { display: grid; grid-template-columns: repeat(auto-fit, minmax(25rem, 1fr)); gap: 1px; background: var(--ui-separator); }
.city-service-type-list article { display: grid; grid-template-columns: minmax(10rem, 0.7fr) minmax(9rem, 0.5fr) 1fr; align-items: center; gap: 0.75rem; padding: 0.75rem 1rem; background: var(--ui-surface); }
.city-service-type-list code, .city-service-type-list strong { display: block; overflow-wrap: anywhere; }
.city-service-type-list strong { margin-top: 0.12rem; font-size: 0.68rem; }
.city-service-type-list > article > span { color: var(--ui-label-secondary); font-size: 0.62rem; }
.city-service-chip-list { display: flex; flex-wrap: wrap; justify-content: flex-end; gap: 0.25rem; }
.city-service-chip-list b { border-left: 2px solid var(--ui-accent); padding: 0.16rem 0.35rem; background: var(--ui-control); font-size: 0.56rem; font-weight: 500; }
.city-service-filters { display: grid; grid-template-columns: repeat(auto-fit, minmax(10rem, 1fr)); align-items: end; gap: 0.7rem; border-bottom: 1px solid var(--ui-separator); padding: 0.75rem 1rem; background: var(--ui-control); }
.city-service-filters label > span, .city-service-form label > span { display: block; margin-bottom: 0.3rem; color: var(--ui-label-secondary); font-size: 0.64rem; }
.city-service-filters > button { min-height: 2.625rem; }
.city-service-table-wrap { overflow-x: auto; }
.city-service-table { width: 100%; min-width: 64rem; border-collapse: collapse; }
.city-service-table th { border-bottom: 1px solid var(--ui-separator); padding: 0.65rem 0.75rem; color: var(--ui-label-secondary); background: var(--ui-control); font-size: 0.62rem; font-weight: 600; text-align: left; }
.city-service-table td { border-bottom: 1px solid var(--ui-separator); padding: 0.75rem; vertical-align: top; font-size: 0.68rem; }
.city-service-table td > strong, .city-service-table td > code, .city-service-table td > small { display: block; }
.city-service-table td > code, .city-service-table td > small { margin-top: 0.15rem; color: var(--ui-label-secondary); font-size: 0.58rem; overflow-wrap: anywhere; }
.city-service-status { display: block; width: fit-content; margin-top: 0.2rem; border-left: 3px solid var(--ui-separator); padding: 0.18rem 0.35rem; color: var(--ui-label-secondary); font-size: 0.58rem; }
.city-service-status[data-status='active'], .city-service-status[data-status='operational'] { border-left-color: #16a36a; color: #2a9b6c; }
.city-service-status[data-status='degraded'], .city-service-status[data-status='suspended'] { border-left-color: #d99b52; color: #c7833c; }
.city-service-status[data-status='retired'], .city-service-status[data-status='offline'] { opacity: 0.7; }
.city-service-capacity-list { display: grid; min-width: 18rem; gap: 1px; background: var(--ui-separator); }
.city-service-capacity-list button { display: grid; grid-template-columns: minmax(8rem, 1fr) auto auto; align-items: center; gap: 0.6rem; padding: 0.42rem 0.5rem; background: var(--ui-control); text-align: left; }
.city-service-capacity-list button:hover:not(:disabled) { background: var(--ui-control-hover); }
.city-service-capacity-list button:disabled { cursor: default; }
.city-service-capacity-list span { font-size: 0.6rem; }
.city-service-capacity-list strong, .city-service-capacity-list small { font: 0.58rem ui-monospace, monospace; }
.city-service-capacity-list small { color: var(--ui-label-secondary); }
.city-service-muted { color: var(--ui-label-secondary); font-size: 0.62rem; }
.city-service-route { display: flex; align-items: center; gap: 0.45rem; }
.city-service-route code { margin: 0 !important; }
.city-service-settlement-list { display: grid; grid-template-columns: repeat(auto-fit, minmax(22rem, 1fr)); gap: 1px; background: var(--ui-separator); }
.city-service-settlement-list > article { min-width: 0; padding: 0.8rem; background: var(--ui-surface); }
.city-service-settlement-list > article > header { display: flex; align-items: flex-start; justify-content: space-between; gap: 0.5rem; }
.city-service-settlement-list header code, .city-service-settlement-list header strong { display: block; }
.city-service-settlement-list header code { color: var(--ui-accent); font-size: 0.58rem; }
.city-service-settlement-list header strong { margin-top: 0.18rem; font-size: 0.72rem; }
.city-service-settlement-list header > span { font: 0.82rem ui-monospace, monospace; }
.city-service-settlement-meter { height: 0.28rem; margin-top: 0.6rem; background: var(--ui-control); }
.city-service-settlement-meter span { display: block; height: 100%; background: var(--ui-accent); }
.city-service-settlement-list dl { display: grid; grid-template-columns: 1fr 1fr; margin: 0.7rem 0 0; border: 1px solid var(--ui-separator); }
.city-service-settlement-list dl div { min-width: 0; padding: 0.45rem 0.55rem; }
.city-service-settlement-list dl div:nth-child(even) { border-left: 1px solid var(--ui-separator); }
.city-service-settlement-list dt { color: var(--ui-label-secondary); font-size: 0.56rem; }
.city-service-settlement-list dd { overflow: hidden; margin: 0.12rem 0 0; font: 0.62rem ui-monospace, monospace; text-overflow: ellipsis; }
.city-service-settlement-list details { margin-top: 0.6rem; border-top: 1px solid var(--ui-separator); padding-top: 0.5rem; }
.city-service-settlement-list summary { cursor: pointer; color: var(--ui-label-secondary); font-size: 0.62rem; }
.city-service-allocation-list { display: grid; margin-top: 0.45rem; gap: 1px; background: var(--ui-separator); }
.city-service-allocation-list > div { display: grid; grid-template-columns: minmax(8rem, 1fr) minmax(10rem, 1fr) auto auto; gap: 0.5rem; padding: 0.4rem 0.5rem; background: var(--ui-control); font-size: 0.57rem; }
.city-service-allocation-list small { color: var(--ui-label-secondary); }
.city-service-load-more { display: flex; align-items: center; justify-content: space-between; border-top: 1px solid var(--ui-separator); padding: 0.65rem 1rem; }
.city-service-load-more span { color: var(--ui-label-secondary); font-size: 0.62rem; }
.city-service-empty { display: grid; min-height: 11rem; place-content: center; justify-items: center; gap: 0.5rem; color: var(--ui-label-secondary); }
.city-service-empty > span { color: var(--ui-accent); font: 1.5rem ui-monospace, monospace; }
.city-service-empty p { margin: 0; font-size: 0.72rem; }
.city-service-empty-list { min-height: 9rem; }
.city-service-unsupported { display: flex; min-height: 11rem; align-items: center; justify-content: center; gap: 1rem; padding: 1rem; }
.city-service-unsupported > code { border: 1px solid var(--ui-separator); padding: 0.75rem; color: var(--ui-accent); background: var(--ui-control); }
.city-service-unsupported strong { font-size: 0.8rem; }
.city-service-unsupported p { max-width: 38rem; margin: 0.25rem 0 0; color: var(--ui-label-secondary); font-size: 0.68rem; }
.city-service-operation-tabs { display: flex; overflow-x: auto; margin: -1rem -1rem 1rem; border-bottom: 1px solid var(--ui-separator); padding: 0 1rem; }
.city-service-operation-tabs button { min-height: 2.65rem; flex: none; border-bottom: 2px solid transparent; padding: 0 0.65rem; color: var(--ui-label-secondary); font-size: 0.64rem; }
.city-service-operation-tabs button.active { border-bottom-color: var(--ui-accent); color: var(--ui-label); background: var(--ui-control); }
.city-service-form { display: grid; grid-template-columns: 1fr 1fr; gap: 0.85rem; }
.city-service-form-note { grid-column: 1 / -1; margin: 0; border-left: 3px solid var(--ui-accent); padding-left: 0.65rem; color: var(--ui-label-secondary); font-size: 0.62rem; }
.city-service-form-preview { display: grid; grid-template-columns: 1fr auto; align-content: center; border: 1px solid var(--ui-separator); padding: 0.55rem 0.65rem; background: var(--ui-control); }
.city-service-form-preview span { color: var(--ui-label-secondary); font-size: 0.58rem; }
.city-service-form-preview strong { grid-row: 2; margin-top: 0.12rem; font: 0.72rem ui-monospace, monospace; }
.city-service-form-preview code { grid-row: 1 / span 2; grid-column: 2; align-self: center; color: var(--ui-label-secondary); font-size: 0.58rem; }
@media (max-width: 1100px) { .city-service-summary { grid-template-columns: repeat(3, 1fr); } .city-service-summary > div:nth-child(3) { border-right: 0; } }
@media (max-width: 720px) { .city-service-header { align-items: flex-start; flex-direction: column; } .city-service-summary { grid-template-columns: repeat(2, 1fr); } .city-service-summary > div:nth-child(3) { border-right: 1px solid var(--ui-separator); } .city-service-summary > div:nth-child(even) { border-right: 0; } .city-service-type-list article, .city-service-allocation-list > div { grid-template-columns: 1fr; } .city-service-chip-list { justify-content: flex-start; } .city-service-form { grid-template-columns: 1fr; } .city-service-form-note { grid-column: auto; } }
</style>
