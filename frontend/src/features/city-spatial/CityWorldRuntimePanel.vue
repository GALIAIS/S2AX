<template>
  <section class="world-runtime-panel" :aria-busy="loading">
    <header class="runtime-header">
      <div>
        <p>{{ t('citySpatial.runtime.eyebrow') }}</p>
        <h2>{{ t('citySpatial.runtime.title') }}</h2>
        <span>{{ t('citySpatial.runtime.description') }}</span>
      </div>
      <dl v-if="catalog" class="runtime-counters">
        <div><dt>{{ t('citySpatial.runtime.counters.actors') }}</dt><dd>{{ catalog.profile.actor_count }}</dd></div>
        <div><dt>{{ t('citySpatial.runtime.counters.facts') }}</dt><dd>{{ catalog.profile.fact_count }}</dd></div>
        <div><dt>{{ t('citySpatial.runtime.counters.cases') }}</dt><dd>{{ catalog.profile.case_count }}</dd></div>
        <div><dt>R</dt><dd>{{ catalog.profile.revision }}</dd></div>
      </dl>
    </header>

    <section class="runtime-membership">
      <div class="runtime-section-heading">
        <div><p>WORLD ACCESS ROSTER</p><h3>{{ t('citySpatial.runtime.members.title') }}</h3></div>
        <span>{{ t('citySpatial.runtime.members.count', { count: members.length }) }}</span>
      </div>
      <form v-if="systemAdmin" class="runtime-member-add" @submit.prevent="addMember">
        <label>
          <span>{{ t('citySpatial.runtime.members.identity') }}</span>
          <input
            v-model.trim="memberForm.identity"
            class="input"
            maxlength="255"
            required
            :placeholder="t('citySpatial.runtime.members.identityPlaceholder')"
          >
        </label>
        <label>
          <span>{{ t('citySpatial.runtime.members.role') }}</span>
          <Select v-model="memberForm.role" :options="memberRoleOptions" :searchable="false" />
        </label>
        <button type="submit" class="btn btn-secondary btn-sm" :disabled="!memberForm.identity || Boolean(memberBusyKey)">
          {{ memberBusyKey === 'add' ? t('citySpatial.runtime.processing') : t('citySpatial.runtime.members.add') }}
        </button>
      </form>
      <div class="runtime-member-list">
        <article v-for="member in members" :key="member.user_id">
          <div class="runtime-member-identity">
            <span>{{ memberInitial(member) }}</span>
            <div>
              <strong>{{ member.username || member.email }}</strong>
              <small>{{ member.username ? member.email : `#${member.user_id}` }}</small>
            </div>
          </div>
          <template v-if="systemAdmin && member.role !== 'owner'">
            <Select
              :model-value="member.role"
              :options="memberRoleOptions"
              :searchable="false"
              :disabled="Boolean(memberBusyKey)"
              @update:model-value="value => updateMemberRole(member.user_id, value)"
            />
            <button
              type="button"
              class="runtime-member-remove"
              :disabled="Boolean(memberBusyKey)"
              @click="removeMember(member.user_id)"
            >
              {{ t('citySpatial.runtime.members.remove') }}
            </button>
          </template>
          <span v-else class="runtime-member-role">{{ memberRoleLabel(member.role) }}</span>
        </article>
      </div>
    </section>

    <section v-if="commandReceipts.length" class="runtime-receipts" aria-live="polite">
      <div class="runtime-section-heading">
        <div><p>COMMAND RECEIPTS</p><h3>{{ t('citySpatial.runtime.receipts.title') }}</h3></div>
        <span>{{ commandReceipts.length }}</span>
      </div>
      <div class="runtime-receipt-list">
        <article v-for="receipt in commandReceipts.slice(0, 8)" :key="receipt.id" :data-status="receipt.status">
          <span class="runtime-receipt-sequence">#{{ receipt.sequence }}</span>
          <strong>{{ prettifyCode(receipt.command_type) }}</strong>
          <span class="runtime-receipt-status">{{ t(`citySpatial.runtime.receipts.status.${receipt.status}`) }}</span>
          <small v-if="receipt.error_code">{{ receipt.error_code }}</small>
          <small v-else-if="receipt.processed_tick !== undefined">T{{ receipt.processed_tick }}</small>
          <small v-else>{{ t('citySpatial.runtime.receipts.awaitingTick') }}</small>
        </article>
      </div>
    </section>

    <div v-if="!catalog" class="runtime-unavailable">
      {{ t('citySpatial.runtime.unavailable') }}
    </div>

    <template v-else>
      <div v-if="actors.length" class="runtime-actor-tabs" role="tablist" :aria-label="t('citySpatial.runtime.actorSelection')">
        <button
          v-for="actor in actors"
          :key="actor.code"
          type="button"
          role="tab"
          :aria-selected="actor.code === selectedActorCode"
          :class="{ active: actor.code === selectedActorCode }"
          @click="emit('selectActor', actor.code)"
        >
          <span>{{ actor.name.slice(0, 1).toUpperCase() }}</span>
          <strong>{{ actor.name }}</strong>
          <small>{{ actor.code }}</small>
        </button>
      </div>

      <form v-if="canCreateActor" class="runtime-character-create" @submit.prevent="createActor">
        <div class="runtime-section-heading">
          <div>
            <p>{{ t('citySpatial.runtime.creation.eyebrow') }}</p>
            <h3>{{ t('citySpatial.runtime.creation.title') }}</h3>
          </div>
          <span>{{ t('citySpatial.runtime.creation.capacity', {
            current: actors.length,
            maximum: catalog.profile.maximum_player_actors_per_member
          }) }}</span>
        </div>
        <div class="runtime-archetypes" role="radiogroup" :aria-label="t('citySpatial.runtime.creation.archetype')">
          <label
            v-for="definition in archetypes"
            :key="definition.code"
            :class="{ active: createForm.archetypeCode === definition.code }"
          >
            <input v-model="createForm.archetypeCode" type="radio" :value="definition.code">
            <span class="runtime-archetype-index">{{ archetypeIndex(definition.code) }}</span>
            <strong>{{ definitionName(definition) }}</strong>
            <small>{{ definitionDescription(definition) }}</small>
            <dl>
              <div v-for="(value, code) in definitionInitialAttributes(definition)" :key="code">
                <dt>{{ definitionLabel('attribute', String(code)) }}</dt>
                <dd>{{ formatScaled(Number(value)) }}</dd>
              </div>
            </dl>
          </label>
        </div>
        <div class="runtime-create-controls">
          <label>
            <span>{{ t('citySpatial.runtime.creation.name') }}</span>
            <input
              v-model.trim="createForm.name"
              class="input"
              maxlength="96"
              required
              :placeholder="t('citySpatial.runtime.creation.namePlaceholder')"
            >
          </label>
          <button
            type="submit"
            class="btn btn-primary"
            :disabled="!createForm.name || !createForm.archetypeCode || Boolean(busyCommandCode)"
          >
            {{ busyCommandCode === 'actor.create'
              ? t('citySpatial.runtime.processing')
              : t('citySpatial.runtime.creation.confirm') }}
          </button>
        </div>
      </form>

      <section v-if="portalAccessAvailability !== 'unavailable'" class="runtime-portals">
        <div class="runtime-section-heading">
          <div><p>PORTAL ACCESS GRAPH</p><h3>{{ t('citySpatial.runtime.portals.title') }}</h3></div>
          <span>{{ portalLoading ? t('citySpatial.runtime.portals.loading') : t('citySpatial.runtime.portals.count', { count: portals.length }) }}</span>
        </div>
        <div v-if="portals.length" class="runtime-portal-list">
          <article
            v-for="portal in portals"
            :key="portalKey(portal)"
            :data-state="portal.state.state_code"
          >
            <header>
              <div>
                <span>{{ portal.state.building_code }}</span>
                <strong>{{ portal.state.portal_code }}</strong>
              </div>
              <div class="runtime-portal-badges">
                <span>{{ portalTypeLabel(portal.state.portal_type) }}</span>
                <span :data-state="portal.state.state_code">{{ portalStateLabel(portal.state.state_code) }}</span>
              </div>
            </header>
            <dl class="runtime-portal-route">
              <div><dt>FROM</dt><dd>{{ formatPortalCoordinate(portal.from) }}</dd></div>
              <div><dt>TO</dt><dd>{{ formatPortalCoordinate(portal.to) }}</dd></div>
              <div><dt>{{ t('citySpatial.runtime.portals.direction') }}</dt><dd>{{ portal.bidirectional ? '↔' : '→' }}</dd></div>
              <div><dt>{{ t('citySpatial.runtime.portals.version') }}</dt><dd>V{{ portal.state.version }} · T{{ portal.state.changed_tick }}</dd></div>
            </dl>
            <div class="runtime-portal-policy">
              <div>
                <span>{{ t('citySpatial.runtime.portals.policy') }}</span>
                <strong>{{ requirementSummary(portal.state.access_requirement) }}</strong>
              </div>
              <div>
                <span>{{ t('citySpatial.runtime.portals.actorAccess') }}</span>
                <strong :data-access="portalAccessStatus(portal)">{{ portalAccessLabel(portal) }}</strong>
              </div>
            </div>
            <p v-if="portal.access_evaluation && !portal.access_evaluation.satisfied" class="runtime-portal-failure">
              {{ portalFailureSummary(portal) }}
            </p>
            <footer>
              <div class="runtime-portal-actions">
                <button
                  v-for="action in portalActions(portal)"
                  :key="action"
                  type="button"
                  class="btn btn-secondary btn-sm"
                  :disabled="!canTransitionPortal(portal) || Boolean(busyCommandCode)"
                  @click="transitionPortal(portal, action)"
                >
                  {{ busyCommandCode === portalCommandCode(portal, action)
                    ? t('citySpatial.runtime.processing')
                    : portalActionLabel(action) }}
                </button>
                <span v-if="portal.state.portal_type !== 'entrance'">{{ t('citySpatial.runtime.portals.fixedOpen') }}</span>
                <span v-else-if="!portalActorInRange(portal)">{{ t('citySpatial.runtime.portals.outOfRange') }}</span>
              </div>
              <button
                v-if="systemAdmin"
                type="button"
                class="runtime-text-action"
                @click="selectPortalPolicy(portal)"
              >
                {{ t('citySpatial.runtime.portals.editPolicy') }}
              </button>
            </footer>
          </article>
        </div>
        <p v-else class="runtime-empty">
          {{ portalLoading ? t('citySpatial.runtime.portals.loading') : t('citySpatial.runtime.portals.empty') }}
        </p>

        <form v-if="systemAdmin && portals.length" class="runtime-portal-policy-form" @submit.prevent="configurePortalPolicy">
          <div class="runtime-portal-policy-heading">
            <span>DECLARATIVE POLICY</span>
            <strong>{{ t('citySpatial.runtime.portals.policyEditor') }}</strong>
            <small>{{ t('citySpatial.runtime.portals.policyHint') }}</small>
          </div>
          <label>
            <span>{{ t('citySpatial.runtime.portals.portal') }}</span>
            <Select v-model="portalPolicyForm.portalKey" :options="portalOptions" :searchable="true" />
          </label>
          <label>
            <span>{{ t('citySpatial.runtime.portals.policyMode') }}</span>
            <Select v-model="portalPolicyForm.mode" :options="portalPolicyModeOptions" :searchable="false" />
          </label>
          <label v-if="portalPolicyDefinitionKind">
            <span>{{ portalPolicyDefinitionLabel }}</span>
            <Select
              v-model="portalPolicyForm.definitionCode"
              :options="portalPolicyDefinitionOptions"
              :searchable="true"
              :empty-text="t('citySpatial.runtime.portals.noDefinitions')"
            />
          </label>
          <label v-if="portalPolicyNeedsValue">
            <span>{{ portalPolicyValueLabel }}</span>
            <input v-model.number="portalPolicyForm.value" class="input font-mono" type="number" min="0" step="0.1">
          </label>
          <label v-if="portalPolicyForm.mode === 'status_present'">
            <span>{{ t('citySpatial.runtime.portals.minimumStacks') }}</span>
            <input v-model.number="portalPolicyForm.minimumStacks" class="input font-mono" type="number" min="0" max="1000000" step="1">
          </label>
          <label v-if="portalPolicyForm.mode === 'fact_count_gte'">
            <span>{{ t('citySpatial.runtime.portals.factType') }}</span>
            <input v-model.trim="portalPolicyForm.factType" class="input font-mono" maxlength="128" placeholder="actor.activity.performed">
          </label>
          <label v-if="portalPolicyForm.mode === 'fact_count_gte'">
            <span>{{ t('citySpatial.runtime.portals.windowTicks') }}</span>
            <input v-model.number="portalPolicyForm.windowTicks" class="input font-mono" type="number" min="1" step="1">
          </label>
          <button
            type="submit"
            class="btn btn-primary btn-sm"
            :disabled="!portalPolicyReady || Boolean(busyCommandCode)"
          >
            {{ busyCommandCode === portalPolicyCommandCode
              ? t('citySpatial.runtime.processing')
              : t('citySpatial.runtime.portals.savePolicy') }}
          </button>
        </form>
      </section>

      <div v-if="actorState" class="runtime-actor-workbench">
        <section class="runtime-identity-card">
          <div class="runtime-avatar">{{ actorState.actor.name.slice(0, 2).toUpperCase() }}</div>
          <div>
            <p>{{ definitionLabel('archetype', actorState.actor.archetype_code ?? '') }}</p>
            <h3>{{ actorState.actor.name }}</h3>
            <span>{{ actorState.actor.code }} · V{{ actorState.actor.version }} · T{{ actorState.actor.updated_tick }}</span>
          </div>
          <div class="runtime-active-roles">
            <span v-for="role in activeRoles" :key="`${role.role_code}-${role.granted_tick}`">
              {{ definitionLabel('role', role.role_code) }}
            </span>
          </div>
        </section>

        <section class="runtime-spatial-control">
          <article class="runtime-location-card">
            <div class="runtime-section-heading">
              <div><p>AUTHORITATIVE LOCATION</p><h3>{{ t('citySpatial.runtime.location.title') }}</h3></div>
              <button
                v-if="actorLocation"
                type="button"
                class="runtime-text-action"
                @click="emit('focusActor', actorState.actor.code)"
              >
                {{ t('citySpatial.runtime.location.showOnMap') }}
              </button>
            </div>
            <template v-if="actorLocation">
              <dl class="runtime-location-facts">
                <div><dt>XYZ</dt><dd>{{ actorLocation.x }}, {{ actorLocation.y }}, {{ actorLocation.z }}</dd></div>
                <div><dt>CHUNK</dt><dd>{{ actorLocation.chunk_x }}, {{ actorLocation.chunk_y }}</dd></div>
                <div><dt>LOCAL</dt><dd>{{ actorLocation.local_x }}, {{ actorLocation.local_y }}</dd></div>
                <div><dt>{{ t('citySpatial.runtime.location.jurisdiction') }}</dt><dd>{{ actorLocation.jurisdiction_code }}</dd></div>
                <div><dt>{{ t('citySpatial.runtime.location.space') }}</dt><dd>{{ actorLocation.space_kind }}:{{ actorLocation.space_code }}</dd></div>
                <div><dt>{{ t('citySpatial.runtime.location.anchor') }}</dt><dd>{{ actorAnchor }}</dd></div>
              </dl>
              <div class="runtime-movement-console">
                <div class="runtime-movement-pad" :aria-label="t('citySpatial.runtime.location.movement')">
                  <button
                    v-for="direction in planarDirections"
                    :key="direction.code"
                    type="button"
                    :class="`runtime-move-${direction.code}`"
                    :title="t(`citySpatial.runtime.location.direction.${direction.code}`)"
                    :aria-label="t(`citySpatial.runtime.location.direction.${direction.code}`)"
                    :disabled="!canCommand || Boolean(busyCommandCode)"
                    @click="moveActor(direction.dx, direction.dy, 0, direction.code)"
                  >
                    {{ direction.glyph }}
                  </button>
                  <span class="runtime-move-center">@</span>
                </div>
                <div class="runtime-vertical-movement">
                  <button
                    type="button"
                    :disabled="!canCommand || Boolean(busyCommandCode)"
                    @click="moveActor(0, 0, 1, 'up')"
                  >
                    <span>+</span>{{ t('citySpatial.runtime.location.levelUp') }}
                  </button>
                  <button
                    type="button"
                    :disabled="!canCommand || Boolean(busyCommandCode)"
                    @click="moveActor(0, 0, -1, 'down')"
                  >
                    <span>−</span>{{ t('citySpatial.runtime.location.levelDown') }}
                  </button>
                  <small>{{ canCommand ? t('citySpatial.runtime.location.movementHint') : t('citySpatial.runtime.control.readOnly') }}</small>
                </div>
              </div>
              <div class="runtime-navigation-console">
                <div class="runtime-navigation-heading">
                  <div>
                    <span>DETERMINISTIC A*</span>
                    <strong>{{ t('citySpatial.runtime.navigation.title') }}</strong>
                  </div>
                  <code>{{ navigationTargetLabel }}</code>
                </div>
                <div class="runtime-navigation-actions">
                  <button
                    type="button"
                    class="btn btn-secondary btn-sm"
                    :disabled="!canPreviewNavigation || navigationLoading || Boolean(busyCommandCode)"
                    @click="emit('previewPath')"
                  >
                    {{ navigationLoading
                      ? t('citySpatial.runtime.navigation.planning')
                      : t('citySpatial.runtime.navigation.preview') }}
                  </button>
                  <button
                    v-if="navigationPath || navigationError"
                    type="button"
                    class="runtime-text-action"
                    @click="emit('clearPath')"
                  >
                    {{ t('citySpatial.runtime.navigation.clear') }}
                  </button>
                </div>
                <p v-if="navigationError" class="runtime-navigation-error" role="alert">{{ navigationError }}</p>
                <template v-else-if="navigationPath">
                  <div class="runtime-navigation-result" :data-reachable="navigationPath.reachable">
                    <dl>
                      <div><dt>{{ t('citySpatial.runtime.navigation.status') }}</dt><dd>{{ navigationStatusLabel }}</dd></div>
                      <div><dt>{{ t('citySpatial.runtime.navigation.steps') }}</dt><dd>{{ navigationMoveCount }}</dd></div>
                      <div><dt>{{ t('citySpatial.runtime.navigation.cost') }}</dt><dd>{{ navigationPath.total_cost }}</dd></div>
                      <div><dt>{{ t('citySpatial.runtime.navigation.expanded') }}</dt><dd>{{ navigationPath.expanded_nodes }}</dd></div>
                    </dl>
                    <button
                      v-if="nextNavigationStep"
                      type="button"
                      class="btn btn-primary btn-sm"
                      :disabled="!canCommand || Boolean(busyCommandCode)"
                      @click="moveToNextNavigationStep"
                    >
                      {{ t('citySpatial.runtime.navigation.moveNext') }}
                      <span>{{ nextNavigationStep.coordinate.x }}, {{ nextNavigationStep.coordinate.y }}, {{ nextNavigationStep.coordinate.z }}</span>
                    </button>
                  </div>
                  <ol v-if="navigationPath.reachable" class="runtime-navigation-steps" :aria-label="t('citySpatial.runtime.navigation.route')">
                    <li
                      v-for="(step, index) in navigationPath.steps.slice(0, 16)"
                      :key="`${step.coordinate.x}:${step.coordinate.y}:${step.coordinate.z}`"
                    >
                      <span>{{ index }}</span>
                      <code>{{ step.coordinate.x }},{{ step.coordinate.y }},{{ step.coordinate.z }}</code>
                      <small>+{{ step.step_cost }}</small>
                    </li>
                    <li v-if="navigationPath.steps.length > 16" class="runtime-navigation-overflow">
                      +{{ navigationPath.steps.length - 16 }}
                    </li>
                  </ol>
                </template>
                <p v-else class="runtime-navigation-hint">{{ t('citySpatial.runtime.navigation.hint') }}</p>

                <section
                  v-if="navigationIntentAvailability !== 'unavailable'"
                  class="runtime-navigation-intent"
                  :aria-busy="navigationIntentLoading"
                >
                  <header>
                    <div>
                      <span>WORLD-TICK MOVEMENT INTENT</span>
                      <strong>{{ t('citySpatial.runtime.navigationIntent.title') }}</strong>
                    </div>
                    <span>{{ navigationIntentLoading
                      ? t('citySpatial.runtime.navigationIntent.loading')
                      : t('citySpatial.runtime.navigationIntent.reservationCount', { count: selectedNavigationReservations.length }) }}</span>
                  </header>

                  <p v-if="navigationIntentError" class="runtime-navigation-intent-error" role="alert">
                    {{ navigationIntentError }}
                  </p>

                  <div
                    v-if="selectedNavigationIntent"
                    class="runtime-navigation-intent-state"
                    :data-status="selectedNavigationIntent.status"
                  >
                    <div class="runtime-navigation-intent-state-heading">
                      <div>
                        <span>{{ selectedNavigationIntent.intent_code }}</span>
                        <strong>{{ navigationIntentStatusLabel }}</strong>
                      </div>
                      <code>V{{ selectedNavigationIntent.version }} · T{{ selectedNavigationIntent.updated_tick }}</code>
                    </div>
                    <dl>
                      <div><dt>{{ t('citySpatial.runtime.navigationIntent.destination') }}</dt><dd>{{ formatNavigationCoordinate(selectedNavigationIntent.destination) }}</dd></div>
                      <div><dt>{{ t('citySpatial.runtime.navigationIntent.nextAttempt') }}</dt><dd>T{{ selectedNavigationIntent.next_attempt_tick }}</dd></div>
                      <div><dt>{{ t('citySpatial.runtime.navigationIntent.priority') }}</dt><dd>{{ signedInteger(selectedNavigationIntent.priority) }}</dd></div>
                      <div><dt>{{ t('citySpatial.runtime.navigationIntent.blockedAttempts') }}</dt><dd>{{ selectedNavigationIntent.blocked_attempts }}</dd></div>
                      <div><dt>{{ t('citySpatial.runtime.navigationIntent.maxSteps') }}</dt><dd>{{ selectedNavigationIntent.max_steps }}</dd></div>
                      <div><dt>{{ t('citySpatial.runtime.navigationIntent.onBlocked') }}</dt><dd>{{ navigationOnBlockedLabel(selectedNavigationIntent.on_blocked) }}</dd></div>
                    </dl>
                    <div class="runtime-navigation-budget">
                      <div>
                        <span>{{ t('citySpatial.runtime.navigationIntent.budget') }}</span>
                        <strong>{{ selectedNavigationIntent.budget_units }} / {{ selectedNavigationIntent.budget_cap_units }}</strong>
                        <small>+{{ selectedNavigationIntent.budget_gain_units }} / TICK</small>
                      </div>
                      <div class="runtime-navigation-budget-meter" role="meter" :aria-valuenow="selectedNavigationIntent.budget_units" :aria-valuemax="selectedNavigationIntent.budget_cap_units">
                        <i :style="{ width: `${navigationBudgetPercent}%` }" />
                      </div>
                    </div>
                    <p v-if="selectedNavigationIntent.last_reason" class="runtime-navigation-intent-reason">
                      {{ navigationIntentReasonLabel(selectedNavigationIntent.last_reason) }}
                    </p>
                  </div>
                  <p v-else-if="!navigationIntentLoading" class="runtime-navigation-intent-empty">
                    {{ t('citySpatial.runtime.navigationIntent.empty') }}
                  </p>

                  <form class="runtime-navigation-intent-form" @submit.prevent="setNavigationIntent">
                    <div class="runtime-navigation-coordinate-fields">
                      <label><span>X</span><input v-model.number="navigationIntentForm.x" class="input font-mono" type="number" step="1"></label>
                      <label><span>Y</span><input v-model.number="navigationIntentForm.y" class="input font-mono" type="number" step="1"></label>
                      <label><span>Z</span><input v-model.number="navigationIntentForm.z" class="input font-mono" type="number" step="1"></label>
                    </div>
                    <label>
                      <span>{{ t('citySpatial.runtime.navigationIntent.priority') }}</span>
                      <input v-model.number="navigationIntentForm.priority" class="input font-mono" type="number" min="-10" max="10" step="1">
                    </label>
                    <label>
                      <span>{{ t('citySpatial.runtime.navigationIntent.maxSteps') }}</span>
                      <input v-model.number="navigationIntentForm.maxSteps" class="input font-mono" type="number" min="1" max="1024" step="1">
                    </label>
                    <label>
                      <span>{{ t('citySpatial.runtime.navigationIntent.onBlocked') }}</span>
                      <Select v-model="navigationIntentForm.onBlocked" :options="navigationOnBlockedOptions" :searchable="false" />
                    </label>
                    <button
                      type="button"
                      class="btn btn-secondary btn-sm"
                      :disabled="!navigationDestination"
                      @click="useSelectedNavigationDestination"
                    >
                      {{ t('citySpatial.runtime.navigationIntent.useSelectedCell') }}
                    </button>
                    <div class="runtime-navigation-intent-form-actions">
                      <button
                        type="submit"
                        class="btn btn-primary btn-sm"
                        :disabled="!navigationIntentReady || Boolean(busyCommandCode)"
                      >
                        {{ busyCommandCode === navigationIntentSetCommandCode
                          ? t('citySpatial.runtime.processing')
                          : selectedNavigationIntent
                            ? t('citySpatial.runtime.navigationIntent.replace')
                            : t('citySpatial.runtime.navigationIntent.create') }}
                      </button>
                      <button
                        v-if="navigationIntentCancellable"
                        type="button"
                        class="btn btn-secondary btn-sm"
                        :disabled="Boolean(busyCommandCode)"
                        @click="cancelNavigationIntent"
                      >
                        {{ busyCommandCode === navigationIntentCancelCommandCode
                          ? t('citySpatial.runtime.processing')
                          : t('citySpatial.runtime.navigationIntent.cancel') }}
                      </button>
                    </div>
                  </form>

                  <article v-if="latestNavigationReservation" class="runtime-navigation-reservation">
                    <span>T{{ latestNavigationReservation.tick }}.{{ latestNavigationReservation.sequence }}</span>
                    <strong>{{ formatNavigationCoordinate(latestNavigationReservation.from) }} → {{ formatNavigationCoordinate(latestNavigationReservation.to) }}</strong>
                    <small>{{ t('citySpatial.runtime.navigationIntent.stepCost', { cost: latestNavigationReservation.step_cost }) }}</small>
                  </article>
                </section>
              </div>
            </template>
            <p v-else class="runtime-empty">{{ t('citySpatial.runtime.location.unavailable') }}</p>
          </article>

          <article class="runtime-control-card">
            <div class="runtime-section-heading">
              <div><p>CONTROL GRANTS</p><h3>{{ t('citySpatial.runtime.control.title') }}</h3></div>
              <span>{{ activeDelegations.length }}</span>
            </div>
            <form v-if="canManageControl" class="runtime-control-form" @submit.prevent="grantControl">
              <label>
                <span>{{ t('citySpatial.runtime.control.memberUserID') }}</span>
                <Select
                  v-model="controlForm.userID"
                  :options="controlMemberOptions"
                  :searchable="true"
                  :placeholder="t('citySpatial.runtime.control.memberUserIDPlaceholder')"
                  :search-placeholder="t('citySpatial.runtime.control.memberSearchPlaceholder')"
                  :empty-text="t('citySpatial.runtime.control.noEligibleMembers')"
                />
              </label>
              <fieldset>
                <legend>{{ t('citySpatial.runtime.control.capabilities') }}</legend>
                <label><input v-model="controlForm.command" type="checkbox">{{ t('citySpatial.runtime.control.actorCommand') }}</label>
                <label><input v-model="controlForm.manage" type="checkbox">{{ t('citySpatial.runtime.control.manageControl') }}</label>
              </fieldset>
              <button
                type="submit"
                class="btn btn-secondary btn-sm"
                :disabled="!controlGrantReady || Boolean(busyCommandCode)"
              >
                {{ t('citySpatial.runtime.control.grant') }}
              </button>
            </form>
            <p v-else class="runtime-control-explanation">{{ t('citySpatial.runtime.control.readOnly') }}</p>
            <div v-if="activeDelegations.length" class="runtime-delegation-list">
              <article v-for="delegation in activeDelegations" :key="delegation.userID">
                <div>
                  <strong>{{ memberDisplayName(delegation.userID) }}</strong>
                  <small>{{ delegation.owner ? t('citySpatial.runtime.control.owner') : t('citySpatial.runtime.control.delegate') }}</small>
                </div>
                <div class="runtime-capability-list">
                  <span v-for="capability in delegation.capabilities" :key="capability">
                    {{ capabilityLabel(capability) }}
                    <button
                      v-if="canManageControl && !delegation.owner"
                      type="button"
                      :aria-label="t('citySpatial.runtime.control.revokeCapability', { capability: capabilityLabel(capability), user: delegation.userID })"
                      :disabled="Boolean(busyCommandCode)"
                      @click="revokeControl(delegation.userID, capability)"
                    >×</button>
                  </span>
                </div>
              </article>
            </div>
            <p v-else class="runtime-empty">{{ t('citySpatial.runtime.control.noDelegations') }}</p>
          </article>
        </section>

        <section class="runtime-attributes">
          <div class="runtime-section-heading">
            <div><p>STATE VECTOR</p><h3>{{ t('citySpatial.runtime.attributes') }}</h3></div>
            <span>{{ t('citySpatial.runtime.serverAuthoritative') }}</span>
          </div>
          <div class="runtime-attribute-grid">
            <article v-for="attribute in actorState.attributes" :key="attribute.attribute_code">
              <header>
                <strong>{{ definitionLabel('attribute', attribute.attribute_code) }}</strong>
                <span>{{ formatScaled(attribute.value_units) }}</span>
              </header>
              <div class="runtime-meter"><i :style="{ width: `${attributePercent(attribute)}%` }" /></div>
              <footer>
                <span>XP {{ attribute.experience_units.toLocaleString() }}</span>
                <span>T{{ attribute.last_changed_tick }} · V{{ attribute.version }}</span>
              </footer>
            </article>
          </div>
        </section>

        <div class="runtime-actions-grid">
          <section>
            <div class="runtime-section-heading">
              <div><p>ACTION CATALOG</p><h3>{{ t('citySpatial.runtime.activities') }}</h3></div>
              <span>{{ t('citySpatial.runtime.activitiesHint') }}</span>
            </div>
            <div class="runtime-activity-list">
              <article v-for="activity in activities" :key="activity.code">
                <div>
                  <strong>{{ definitionName(activity) }}</strong>
                  <p>{{ definitionDescription(activity) }}</p>
                  <small>{{ activityEffectSummary(activity) }}</small>
                </div>
                <button
                  type="button"
                  class="btn btn-secondary btn-sm"
                  :disabled="!canCommand || Boolean(busyCommandCode)"
                  @click="performActivity(activity.code)"
                >
                  {{ busyCommandCode === `activity:${activity.code}`
                    ? t('citySpatial.runtime.processing')
                    : t('citySpatial.runtime.perform') }}
                </button>
              </article>
            </div>
          </section>

          <section>
            <div class="runtime-section-heading">
              <div><p>ROLE GRAPH</p><h3>{{ t('citySpatial.runtime.roles') }}</h3></div>
              <span>{{ t('citySpatial.runtime.rolesHint') }}</span>
            </div>
            <div class="runtime-role-list">
              <article v-for="option in roleOptions" :key="option.definition.code" :class="{ active: option.active }">
                <div>
                  <strong>{{ definitionName(option.definition) }}</strong>
                  <p>{{ definitionDescription(option.definition) }}</p>
                  <small v-if="option.active">{{ t('citySpatial.runtime.roleState.active') }}</small>
                  <small v-else-if="option.eligible">{{ t('citySpatial.runtime.roleState.eligible') }}</small>
                  <small v-else>{{ roleBlockSummary(option) }}</small>
                </div>
                <button
                  v-if="!option.active"
                  type="button"
                  class="btn btn-secondary btn-sm"
                  :disabled="!canCommand || !option.eligible || Boolean(busyCommandCode)"
                  @click="transitionRole(option.definition.code)"
                >
                  {{ busyCommandCode === `role:${option.definition.code}`
                    ? t('citySpatial.runtime.processing')
                    : t('citySpatial.runtime.transition') }}
                </button>
                <span v-else class="runtime-active-mark">ACTIVE</span>
              </article>
            </div>
          </section>
        </div>

        <div class="runtime-governance-grid">
          <section>
            <div class="runtime-section-heading">
              <div><p>STATUS LEDGER</p><h3>{{ t('citySpatial.runtime.statuses') }}</h3></div>
              <span>{{ activeStatuses.length }}</span>
            </div>
            <div v-if="actorState.statuses.length" class="runtime-status-list">
              <article v-for="status in actorState.statuses" :key="status.instance_code" :data-lifecycle="status.lifecycle_status">
                <header>
                  <strong>{{ definitionLabel('status', status.status_code) }}</strong>
                  <span>{{ status.lifecycle_status }}</span>
                </header>
                <p>{{ t('citySpatial.runtime.statusSummary', {
                  stacks: status.stacks,
                  intensity: formatScaled(status.intensity_units)
                }) }}</p>
                <footer>T{{ status.granted_tick }} → {{ status.expires_tick ? `T${status.expires_tick}` : '∞' }}</footer>
              </article>
            </div>
            <p v-else class="runtime-empty">{{ t('citySpatial.runtime.noStatuses') }}</p>
          </section>

          <section>
            <div class="runtime-section-heading">
              <div><p>RULE CASES</p><h3>{{ t('citySpatial.runtime.cases') }}</h3></div>
              <span>{{ actorCases.length }}</span>
            </div>
            <div v-if="actorCases.length" class="runtime-case-list">
              <article v-for="item in actorCases.slice(0, 12)" :key="item.code">
                <span>T{{ item.tick }}.{{ item.sequence }}</span>
                <strong>{{ definitionLabel('rule', item.rule_code) }}</strong>
                <small>{{ item.decision_code ?? item.status }} · {{ formatScaled(item.severity_units) }}</small>
              </article>
            </div>
            <p v-else class="runtime-empty">{{ t('citySpatial.runtime.noCases') }}</p>
          </section>

          <section>
            <div class="runtime-section-heading">
              <div><p>PUBLIC RULEBOOK</p><h3>{{ t('citySpatial.runtime.rules') }}</h3></div>
              <span>{{ rules.length }}</span>
            </div>
            <div class="runtime-rule-list">
              <article v-for="rule in rules" :key="rule.code">
                <strong>{{ definitionName(rule) }}</strong>
                <p>{{ definitionDescription(rule) }}</p>
                <small>{{ ruleScope(rule) }}</small>
              </article>
            </div>
          </section>
        </div>

        <section class="runtime-fact-stream">
          <div class="runtime-section-heading">
            <div><p>IMMUTABLE FACT STREAM</p><h3>{{ t('citySpatial.runtime.facts') }}</h3></div>
            <span>{{ actorState.recent_facts.length }}</span>
          </div>
          <div v-if="actorState.recent_facts.length">
            <article v-for="fact in actorState.recent_facts.slice(0, 16)" :key="`${fact.tick}-${fact.sequence}`">
              <span>T{{ fact.tick }}.{{ fact.sequence }}</span>
              <strong>{{ factTypeLabel(fact.fact_type) }}</strong>
              <small>{{ fact.definition_code ? definitionLabel(fact.definition_kind ?? '', fact.definition_code) : 'SYSTEM' }}</small>
            </article>
          </div>
        </section>
      </div>
    </template>

    <div v-if="loading" class="runtime-loading-line" aria-hidden="true" />
  </section>
</template>

<script setup lang="ts">
import { computed, reactive, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import type {
  AddCityWorldMemberRequest,
  CityCommand,
  CityMember,
  CityMemberRole,
  CityNavigationCoordinate,
  CityNavigationPath,
  UpdateCityWorldMemberRequest,
  WorldActor,
  WorldActorAttribute,
  WorldActorCapability,
  WorldActorNavigationIntent,
  WorldActorRoleOption,
  WorldActorState,
  WorldNavigationOnBlocked,
  WorldNavigationReservation,
  WorldPortalAccessView,
  WorldPortalAction,
  WorldPortalStateCode,
  WorldRequirementNode,
  WorldRuleCase,
  WorldRuntimeCatalog,
  WorldRuntimeCommandType,
  WorldRuntimeDefinition
} from '@/api/citySpatial'
import Select, { type SelectOption } from '@/components/common/Select.vue'

const props = withDefaults(defineProps<{
  catalog: WorldRuntimeCatalog | null
  actors: WorldActor[]
  selectedActorCode: string | null
  actorState: WorldActorState | null
  roleOptions: WorldActorRoleOption[]
  rules: WorldRuntimeDefinition[]
  cases: WorldRuleCase[]
  members: CityMember[]
  commandReceipts: CityCommand[]
  memberRole: string
  systemAdmin?: boolean
  loading: boolean
  busyCommandCode: string | null
  memberBusyKey: string | null
  navigationPath?: CityNavigationPath | null
  navigationLoading?: boolean
  navigationError?: string | null
  navigationDestination?: CityNavigationCoordinate | null
  navigationIntents?: WorldActorNavigationIntent[]
  navigationReservations?: WorldNavigationReservation[]
  navigationIntentAvailability?: 'unknown' | 'available' | 'unavailable'
  navigationIntentLoading?: boolean
  navigationIntentError?: string | null
  portals?: WorldPortalAccessView[]
  portalAccessAvailability?: 'unknown' | 'available' | 'unavailable'
  portalLoading?: boolean
}>(), {
  navigationPath: null,
  navigationLoading: false,
  navigationError: null,
  navigationDestination: null,
  navigationIntents: () => [],
  navigationReservations: () => [],
  navigationIntentAvailability: 'unknown',
  navigationIntentLoading: false,
  navigationIntentError: null,
  portals: () => [],
  portalAccessAvailability: 'unknown',
  portalLoading: false,
  systemAdmin: false
})

const emit = defineEmits<{
  selectActor: [actorCode: string]
  focusActor: [actorCode: string]
  previewPath: []
  clearPath: []
  command: [commandType: WorldRuntimeCommandType, payload: Record<string, unknown>, commandCode: string]
  memberAdd: [request: AddCityWorldMemberRequest]
  memberUpdate: [userID: number, request: UpdateCityWorldMemberRequest]
}>()

const { t, te } = useI18n()
const createForm = reactive({ name: '', archetypeCode: '' })
const controlForm = reactive<{ userID: number | null; command: boolean; manage: boolean }>({
  userID: null,
  command: true,
  manage: false
})
const memberForm = reactive<{ identity: string; role: Exclude<CityMemberRole, 'owner'> }>({
  identity: '',
  role: 'viewer'
})
const navigationIntentForm = reactive<{
  x: number
  y: number
  z: number
  priority: number
  maxSteps: number
  onBlocked: WorldNavigationOnBlocked
}>({
  x: 0,
  y: 0,
  z: 0,
  priority: 0,
  maxSteps: 256,
  onBlocked: 'retry'
})
type PortalPolicyMode =
  | 'unchanged'
  | 'public'
  | 'attribute_gte'
  | 'attribute_lte'
  | 'experience_gte'
  | 'role_active'
  | 'role_inactive'
  | 'status_present'
  | 'status_absent'
  | 'fact_count_gte'
  | 'world_tick_gte'

const portalPolicyForm = reactive<{
  portalKey: string
  mode: PortalPolicyMode
  definitionCode: string
  value: number
  minimumStacks: number
  factType: string
  windowTicks: number
}>({
  portalKey: '',
  mode: 'unchanged',
  definitionCode: '',
  value: 0,
  minimumStacks: 1,
  factType: '',
  windowTicks: 100
})

const memberRoleOptions = computed<SelectOption[]>(() => [
  { value: 'planner', label: t('citySpatial.runtime.members.roles.planner') },
  { value: 'treasurer', label: t('citySpatial.runtime.members.roles.treasurer') },
  { value: 'trader', label: t('citySpatial.runtime.members.roles.trader') },
  { value: 'viewer', label: t('citySpatial.runtime.members.roles.viewer') }
])

const planarDirections = [
  { code: 'northWest', dx: -1, dy: -1, glyph: '↖' },
  { code: 'north', dx: 0, dy: -1, glyph: '↑' },
  { code: 'northEast', dx: 1, dy: -1, glyph: '↗' },
  { code: 'west', dx: -1, dy: 0, glyph: '←' },
  { code: 'east', dx: 1, dy: 0, glyph: '→' },
  { code: 'southWest', dx: -1, dy: 1, glyph: '↙' },
  { code: 'south', dx: 0, dy: 1, glyph: '↓' },
  { code: 'southEast', dx: 1, dy: 1, glyph: '↘' }
] as const

const definitions = computed(() => props.catalog?.definitions ?? [])
const archetypes = computed(() => definitions.value.filter(item => item.kind === 'archetype'))
const activities = computed(() => definitions.value.filter(item => item.kind === 'activity'))
const activeRoles = computed(() => props.actorState?.roles.filter(item => item.status === 'active') ?? [])
const activeStatuses = computed(() => props.actorState?.statuses.filter(item => item.lifecycle_status === 'active') ?? [])
const actorCases = computed(() => props.cases.filter(item => item.subject_actor_code === props.selectedActorCode))
const canCreateActor = computed(() => Boolean(
  props.catalog && props.actors.length < props.catalog.profile.maximum_player_actors_per_member
))
const actorLocation = computed(() => props.actorState?.location ?? props.actorState?.actor.location ?? null)
const actorCapabilities = computed(() => props.actorState?.capabilities ?? [])
const canCommand = computed(() => props.systemAdmin || actorCapabilities.value.includes('actor.command'))
const canManageControl = computed(() => props.systemAdmin)
const actorAnchor = computed(() => {
  const location = actorLocation.value
  if (!location?.anchor_kind || !location.anchor_code) return t('citySpatial.runtime.location.noAnchor')
  return `${location.anchor_kind}:${location.anchor_code}`
})
const canPreviewNavigation = computed(() => Boolean(
  canCommand.value && actorLocation.value && props.navigationDestination
))
const navigationTargetLabel = computed(() => {
  const destination = props.navigationDestination
  return destination
    ? `${destination.x}, ${destination.y}, ${destination.z}`
    : t('citySpatial.runtime.navigation.noTarget')
})
const navigationMoveCount = computed(() => Math.max(0, (props.navigationPath?.steps.length ?? 1) - 1))
const nextNavigationStep = computed(() => (
  props.navigationPath?.reachable ? (props.navigationPath.steps[1] ?? null) : null
))
const navigationStatusLabel = computed(() => {
  const path = props.navigationPath
  if (!path) return ''
  if (path.reachable) {
    return path.steps.length <= 1
      ? t('citySpatial.runtime.navigation.arrived')
      : t('citySpatial.runtime.navigation.reachable')
  }
  const reasonKey = `citySpatial.runtime.navigation.reasons.${path.reason ?? 'unreachable'}`
  return te(reasonKey) ? t(reasonKey) : (path.reason ?? t('citySpatial.runtime.navigation.unreachable'))
})
const selectedNavigationIntent = computed(() => (
  props.navigationIntents.find(intent => intent.actor_code === props.selectedActorCode)
    ?? props.actorState?.navigation_intent
    ?? null
))
const selectedNavigationReservations = computed(() => props.navigationReservations
  .filter(reservation => reservation.actor_code === props.selectedActorCode)
  .sort((left, right) => right.tick - left.tick || right.sequence - left.sequence))
const latestNavigationReservation = computed(() => selectedNavigationReservations.value[0] ?? null)
const navigationBudgetPercent = computed(() => {
  const intent = selectedNavigationIntent.value
  if (!intent || intent.budget_cap_units <= 0) return 0
  return Math.max(0, Math.min(100, intent.budget_units / intent.budget_cap_units * 100))
})
const navigationOnBlockedOptions = computed<SelectOption[]>(() => [
  { value: 'retry', label: t('citySpatial.runtime.navigationIntent.onBlockedOptions.retry') },
  { value: 'cancel', label: t('citySpatial.runtime.navigationIntent.onBlockedOptions.cancel') }
])
const navigationIntentCancellable = computed(() => (
  selectedNavigationIntent.value?.status === 'active' || selectedNavigationIntent.value?.status === 'blocked'
))
const navigationIntentReady = computed(() => {
  if (!props.selectedActorCode || !canCommand.value || props.navigationIntentAvailability !== 'available') return false
  const { x, y, z, priority, maxSteps } = navigationIntentForm
  if (![x, y, z, priority, maxSteps].every(Number.isSafeInteger)) return false
  if (priority < -10 || priority > 10 || maxSteps < 1 || maxSteps > 1024) return false
  const current = selectedNavigationIntent.value
  if (!current || (current.status !== 'active' && current.status !== 'blocked')) return true
  return current.destination.x !== x || current.destination.y !== y || current.destination.z !== z ||
    current.priority !== priority || current.max_steps !== maxSteps || current.on_blocked !== navigationIntentForm.onBlocked
})
const navigationIntentStatusLabel = computed(() => {
  const status = selectedNavigationIntent.value?.status
  return status ? t(`citySpatial.runtime.navigationIntent.statuses.${status}`) : ''
})
const navigationIntentSetCommandCode = computed(() => (
  `navigation:intent:set:${props.selectedActorCode ?? 'actor'}`
))
const navigationIntentCancelCommandCode = computed(() => (
  `navigation:intent:cancel:${props.selectedActorCode ?? 'actor'}`
))
const controlGrantReady = computed(() => {
  const userID = Number(controlForm.userID)
  return Number.isSafeInteger(userID) && userID > 0 && (controlForm.command || controlForm.manage)
})
const controlMemberOptions = computed<SelectOption[]>(() => props.members
  .filter(member => member.role !== 'owner' && member.status === 'active')
  .map(member => ({
    value: member.user_id,
    label: member.username
      ? `${member.username} · ${member.email}`
      : `${member.email} · #${member.user_id}`,
    description: `${member.email} ${member.username} ${member.user_id}`
  })))
const activeDelegations = computed(() => {
  const actor = props.actorState?.actor
  const grants = props.actorState?.control_grants ?? []
  const grouped = new Map<number, { userID: number; owner: boolean; capabilities: WorldActorCapability[] }>()
  for (const grant of grants) {
    if (grant.status !== 'active') continue
    const current = grouped.get(grant.user_id) ?? {
      userID: grant.user_id,
      owner: grant.user_id === actor?.owner_user_id,
      capabilities: []
    }
    if (!current.capabilities.includes(grant.capability)) current.capabilities.push(grant.capability)
    grouped.set(grant.user_id, current)
  }
  return [...grouped.values()]
    .map(item => ({ ...item, capabilities: [...item.capabilities].sort() }))
    .sort((left, right) => Number(right.owner) - Number(left.owner) || left.userID - right.userID)
})
const portalOptions = computed<SelectOption[]>(() => props.portals.map(portal => ({
  value: portalKey(portal),
  label: `${portal.state.building_code} / ${portal.state.portal_code}`,
  description: `${portal.state.portal_type} ${formatPortalCoordinate(portal.from)} ${formatPortalCoordinate(portal.to)}`
})))
const selectedPortalForPolicy = computed(() => (
  props.portals.find(portal => portalKey(portal) === portalPolicyForm.portalKey) ?? null
))
const portalPolicyModeOptions = computed<SelectOption[]>(() => [
  { value: 'unchanged', label: t('citySpatial.runtime.portals.policyModes.unchanged') },
  { value: 'public', label: t('citySpatial.runtime.portals.policyModes.public') },
  { value: 'role_active', label: t('citySpatial.runtime.portals.policyModes.roleActive') },
  { value: 'role_inactive', label: t('citySpatial.runtime.portals.policyModes.roleInactive') },
  { value: 'attribute_gte', label: t('citySpatial.runtime.portals.policyModes.attributeGte') },
  { value: 'attribute_lte', label: t('citySpatial.runtime.portals.policyModes.attributeLte') },
  { value: 'experience_gte', label: t('citySpatial.runtime.portals.policyModes.experienceGte') },
  { value: 'status_present', label: t('citySpatial.runtime.portals.policyModes.statusPresent') },
  { value: 'status_absent', label: t('citySpatial.runtime.portals.policyModes.statusAbsent') },
  { value: 'fact_count_gte', label: t('citySpatial.runtime.portals.policyModes.factCountGte') },
  { value: 'world_tick_gte', label: t('citySpatial.runtime.portals.policyModes.worldTickGte') }
])
const portalPolicyDefinitionKind = computed<'attribute' | 'role' | 'status' | null>(() => {
  if (['attribute_gte', 'attribute_lte', 'experience_gte'].includes(portalPolicyForm.mode)) return 'attribute'
  if (['role_active', 'role_inactive'].includes(portalPolicyForm.mode)) return 'role'
  if (['status_present', 'status_absent'].includes(portalPolicyForm.mode)) return 'status'
  return null
})
const portalPolicyDefinitionOptions = computed<SelectOption[]>(() => {
  const kind = portalPolicyDefinitionKind.value
  if (!kind) return []
  return definitions.value
    .filter(definition => definition.kind === kind)
    .map(definition => ({ value: definition.code, label: definitionName(definition), description: definition.code }))
})
const portalPolicyDefinitionLabel = computed(() => {
  const kind = portalPolicyDefinitionKind.value
  return kind ? t(`citySpatial.runtime.portals.definitionKinds.${kind}`) : ''
})
const portalPolicyNeedsValue = computed(() => [
  'attribute_gte', 'attribute_lte', 'experience_gte', 'fact_count_gte', 'world_tick_gte'
].includes(portalPolicyForm.mode))
const portalPolicyValueLabel = computed(() => (
  ['attribute_gte', 'attribute_lte', 'experience_gte'].includes(portalPolicyForm.mode)
    ? t('citySpatial.runtime.portals.scaledThreshold')
    : t('citySpatial.runtime.portals.integerThreshold')
))
const portalPolicyRequirement = computed(() => buildPortalPolicyRequirement())
const portalPolicyReady = computed(() => {
  const portal = selectedPortalForPolicy.value
  const requirement = portalPolicyRequirement.value
  return Boolean(
    props.systemAdmin && portal && requirement &&
    requirementIdentity(requirement) !== requirementIdentity(portal.state.access_requirement)
  )
})
const portalPolicyCommandCode = computed(() => {
  const portal = selectedPortalForPolicy.value
  return portal ? `portal:policy:${portalKey(portal)}` : 'portal:policy'
})

watch(archetypes, items => {
  if (!items.some(item => item.code === createForm.archetypeCode)) {
    createForm.archetypeCode = items[0]?.code ?? ''
  }
}, { immediate: true })

watch(() => {
  const intent = selectedNavigationIntent.value
  return `${props.selectedActorCode ?? ''}:${intent?.intent_code ?? ''}:${intent?.version ?? 0}`
}, () => hydrateNavigationIntentForm(), { immediate: true })

watch(() => {
  const destination = props.navigationDestination
  return destination ? `${destination.x}:${destination.y}:${destination.z}` : ''
}, () => {
  if (!selectedNavigationIntent.value) useSelectedNavigationDestination()
}, { immediate: true })

watch(() => props.portals.map(portal => `${portalKey(portal)}:${portal.state.access_policy_hash}`), keys => {
  if (!keys.length) {
    portalPolicyForm.portalKey = ''
    portalPolicyForm.mode = 'unchanged'
    return
  }
  const selected = props.portals.find(portal => portalKey(portal) === portalPolicyForm.portalKey)
  if (selected) return
  const first = props.portals[0]
  if (first) selectPortalPolicy(first)
}, { immediate: true })

watch(portalPolicyDefinitionOptions, options => {
  if (!options.some(option => option.value === portalPolicyForm.definitionCode)) {
    portalPolicyForm.definitionCode = String(options[0]?.value ?? '')
  }
}, { immediate: true })

function hydrateNavigationIntentForm(): void {
  const intent = selectedNavigationIntent.value
  const destination = intent?.destination ?? props.navigationDestination ?? actorLocation.value
  if (destination) {
    navigationIntentForm.x = destination.x
    navigationIntentForm.y = destination.y
    navigationIntentForm.z = destination.z
  }
  navigationIntentForm.priority = intent?.priority ?? 0
  navigationIntentForm.maxSteps = intent?.max_steps ?? 256
  navigationIntentForm.onBlocked = intent?.on_blocked ?? 'retry'
}

function useSelectedNavigationDestination(): void {
  const destination = props.navigationDestination
  if (!destination) return
  navigationIntentForm.x = destination.x
  navigationIntentForm.y = destination.y
  navigationIntentForm.z = destination.z
}

function formatNavigationCoordinate(coordinate: CityNavigationCoordinate): string {
  return `${coordinate.x}, ${coordinate.y}, ${coordinate.z}`
}

function signedInteger(value: number): string {
  return value > 0 ? `+${value}` : String(value)
}

function navigationOnBlockedLabel(value: WorldNavigationOnBlocked): string {
  return t(`citySpatial.runtime.navigationIntent.onBlockedOptions.${value}`)
}

function navigationIntentReasonLabel(reason: string): string {
  const key = `citySpatial.runtime.navigationIntent.reasons.${reason}`
  return te(key) ? t(key) : prettifyCode(reason)
}

function payloadRecord(definition: WorldRuntimeDefinition): Record<string, unknown> {
  return definition.payload && typeof definition.payload === 'object' ? definition.payload : {}
}

function definitionName(definition: WorldRuntimeDefinition): string {
  const key = String(payloadRecord(definition).name_key ?? '')
  return key && te(key) ? t(key) : prettifyCode(definition.code)
}

function definitionDescription(definition: WorldRuntimeDefinition): string {
  const key = String(payloadRecord(definition).description_key ?? '')
  return key && te(key) ? t(key) : definition.code
}

function definitionLabel(kind: string, code: string): string {
  if (!code) return t('citySpatial.runtime.unknown')
  const definition = definitions.value.find(item => item.kind === kind && item.code === code)
    ?? props.rules.find(item => item.kind === kind && item.code === code)
  return definition ? definitionName(definition) : prettifyCode(code)
}

function prettifyCode(code: string): string {
  const parts = code.split('.')
  const tail = parts[parts.length - 1] ?? code
  return tail.split('_').join(' ').replace(/\b\w/g, (value: string) => value.toUpperCase())
}

function portalKey(portal: WorldPortalAccessView): string {
  return `${portal.state.building_code}/${portal.state.portal_code}`
}

function formatPortalCoordinate(coordinate: CityNavigationCoordinate): string {
  return `${coordinate.x}, ${coordinate.y}, ${coordinate.z}`
}

function portalTypeLabel(portalType: string): string {
  const key = `citySpatial.portalType.${portalType}`
  return te(key) ? t(key) : prettifyCode(portalType)
}

function portalStateLabel(state: WorldPortalStateCode): string {
  return t(`citySpatial.runtime.portals.states.${state}`)
}

function portalActionLabel(action: WorldPortalAction): string {
  return t(`citySpatial.runtime.portals.actions.${action}`)
}

function portalActions(portal: WorldPortalAccessView): WorldPortalAction[] {
  if (portal.state.portal_type !== 'entrance') return []
  switch (portal.state.state_code) {
    case 'open': return ['close']
    case 'closed': return ['open', 'lock']
    case 'locked': return ['unlock']
  }
}

function portalActorInRange(portal: WorldPortalAccessView): boolean {
  const location = actorLocation.value
  if (!location) return false
  return [portal.from, portal.to].some(endpoint => (
    location.z === endpoint.z &&
    Math.abs(location.x - endpoint.x) <= 1 &&
    Math.abs(location.y - endpoint.y) <= 1
  ))
}

function canTransitionPortal(portal: WorldPortalAccessView): boolean {
  return Boolean(
    props.selectedActorCode && canCommand.value && portal.state.portal_type === 'entrance' &&
    portalActorInRange(portal) && portal.access_evaluation?.satisfied === true
  )
}

function portalCommandCode(portal: WorldPortalAccessView, action: WorldPortalAction): string {
  return `portal:state:${portalKey(portal)}:${action}`
}

function transitionPortal(portal: WorldPortalAccessView, action: WorldPortalAction): void {
  if (!props.selectedActorCode || !canTransitionPortal(portal) || !portalActions(portal).includes(action)) return
  emit('command', 'portal.state.transition', {
    actor_code: props.selectedActorCode,
    building_code: portal.state.building_code,
    portal_code: portal.state.portal_code,
    action
  }, portalCommandCode(portal, action))
}

function portalAccessStatus(portal: WorldPortalAccessView): 'allowed' | 'denied' | 'closed' | 'locked' | 'notEvaluated' {
  if (portal.state.state_code === 'locked') return 'locked'
  if (portal.state.state_code === 'closed') return 'closed'
  if (!portal.access_evaluation) return 'notEvaluated'
  return portal.access_evaluation.satisfied ? 'allowed' : 'denied'
}

function portalAccessLabel(portal: WorldPortalAccessView): string {
  return t(`citySpatial.runtime.portals.access.${portalAccessStatus(portal)}`)
}

function requirementSummary(requirement: WorldRequirementNode): string {
  const items = requirement.items ?? []
  switch (requirement.op) {
    case 'all':
      return items.length
        ? items.map(requirementSummary).join(` ${t('citySpatial.runtime.portals.requirements.and')} `)
        : t('citySpatial.runtime.portals.requirements.public')
    case 'any':
      return items.map(requirementSummary).join(` ${t('citySpatial.runtime.portals.requirements.or')} `)
    case 'not':
      return `${t('citySpatial.runtime.portals.requirements.not')} (${requirement.item ? requirementSummary(requirement.item) : '—'})`
    case 'attribute_gte':
      return `${definitionLabel('attribute', requirement.attribute_code ?? '')} ≥ ${formatScaled(requirement.value_units ?? 0)}`
    case 'attribute_lte':
      return `${definitionLabel('attribute', requirement.attribute_code ?? '')} ≤ ${formatScaled(requirement.value_units ?? 0)}`
    case 'experience_gte':
      return `${definitionLabel('attribute', requirement.attribute_code ?? '')} XP ≥ ${formatScaled(requirement.value_units ?? 0)}`
    case 'role_active':
      return `${definitionLabel('role', requirement.role_code ?? '')} · ${t('citySpatial.runtime.portals.requirements.active')}`
    case 'role_inactive':
      return `${definitionLabel('role', requirement.role_code ?? '')} · ${t('citySpatial.runtime.portals.requirements.inactive')}`
    case 'status_present':
      return `${definitionLabel('status', requirement.status_code ?? '')} ≥ ${requirement.minimum_stacks ?? 0}`
    case 'status_absent':
      return `${definitionLabel('status', requirement.status_code ?? '')} · ${t('citySpatial.runtime.portals.requirements.absent')}`
    case 'fact_count_gte':
      return `${requirement.fact_type ?? '—'} ≥ ${requirement.value_units ?? 0} / ${requirement.window_ticks ?? 0}T`
    case 'world_tick_gte':
      return `T ≥ ${requirement.value_units ?? 0}`
  }
}

function portalFailureSummary(portal: WorldPortalAccessView): string {
  const failure = portal.access_evaluation?.failures[0]
  if (!failure) return t('citySpatial.runtime.portals.access.denied')
  const kind = failure.operator.startsWith('role_')
    ? 'role'
    : failure.operator.startsWith('status_')
      ? 'status'
      : failure.operator.startsWith('attribute_') || failure.operator.startsWith('experience_')
        ? 'attribute'
        : ''
  const condition = failure.code
    ? definitionLabel(kind, failure.code)
    : prettifyCode(failure.message_code)
  if (failure.required_units === undefined) return condition
  const scaled = kind === 'attribute'
  return t('citySpatial.runtime.portals.requirementFailure', {
    condition,
    actual: scaled ? formatScaled(failure.actual_units ?? 0) : (failure.actual_units ?? 0),
    required: scaled ? formatScaled(failure.required_units) : failure.required_units
  })
}

function requirementIdentity(requirement: WorldRequirementNode): string {
  switch (requirement.op) {
    case 'all':
    case 'any':
      return `${requirement.op}[${(requirement.items ?? []).map(requirementIdentity).join(',')}]`
    case 'not':
      return `not(${requirement.item ? requirementIdentity(requirement.item) : ''})`
    case 'attribute_gte':
    case 'attribute_lte':
    case 'experience_gte':
      return `${requirement.op}:${requirement.attribute_code ?? ''}:${requirement.value_units ?? 0}`
    case 'role_active':
    case 'role_inactive':
      return `${requirement.op}:${requirement.role_code ?? ''}`
    case 'status_present':
      return `${requirement.op}:${requirement.status_code ?? ''}:${requirement.minimum_stacks ?? 0}`
    case 'status_absent':
      return `${requirement.op}:${requirement.status_code ?? ''}`
    case 'fact_count_gte':
      return `${requirement.op}:${requirement.fact_type ?? ''}:${requirement.value_units ?? 0}:${requirement.window_ticks ?? 0}`
    case 'world_tick_gte':
      return `${requirement.op}:${requirement.value_units ?? 0}`
  }
}

function buildPortalPolicyRequirement(): WorldRequirementNode | null {
  const mode = portalPolicyForm.mode
  if (mode === 'unchanged') return null
  if (mode === 'public') return { op: 'all' }
  const definitionCode = portalPolicyForm.definitionCode.trim().toLowerCase()
  if (['attribute_gte', 'attribute_lte', 'experience_gte'].includes(mode)) {
    const value = Number(portalPolicyForm.value)
    if (!definitionCode || !Number.isFinite(value) || value < 0) return null
    return { op: mode, attribute_code: definitionCode, value_units: Math.round(value * 1000) }
  }
  if (mode === 'role_active' || mode === 'role_inactive') {
    return definitionCode ? { op: mode, role_code: definitionCode } : null
  }
  if (mode === 'status_present') {
    const minimumStacks = Number(portalPolicyForm.minimumStacks)
    if (!definitionCode || !Number.isSafeInteger(minimumStacks) || minimumStacks < 0 || minimumStacks > 1000000) return null
    return { op: mode, status_code: definitionCode, ...(minimumStacks ? { minimum_stacks: minimumStacks } : {}) }
  }
  if (mode === 'status_absent') {
    return definitionCode ? { op: mode, status_code: definitionCode } : null
  }
  if (mode === 'fact_count_gte') {
    const factType = portalPolicyForm.factType.trim().toLowerCase()
    const value = Number(portalPolicyForm.value)
    const windowTicks = Number(portalPolicyForm.windowTicks)
    if (!/^[a-z0-9][a-z0-9._-]{0,127}$/.test(factType) || !Number.isSafeInteger(value) || value < 0 ||
      !Number.isSafeInteger(windowTicks) || windowTicks < 1) return null
    return { op: mode, fact_type: factType, ...(value ? { value_units: value } : {}), window_ticks: windowTicks }
  }
  const value = Number(portalPolicyForm.value)
  if (!Number.isSafeInteger(value) || value < 0) return null
  return { op: 'world_tick_gte', ...(value ? { value_units: value } : {}) }
}

function selectPortalPolicy(portal: WorldPortalAccessView): void {
	if (!props.systemAdmin) return
  const requirement = portal.state.access_requirement
  portalPolicyForm.portalKey = portalKey(portal)
  portalPolicyForm.mode = 'unchanged'
  portalPolicyForm.definitionCode = ''
  portalPolicyForm.value = 0
  portalPolicyForm.minimumStacks = 1
  portalPolicyForm.factType = ''
  portalPolicyForm.windowTicks = 100
  if (requirement.op === 'all' && !(requirement.items?.length)) {
    portalPolicyForm.mode = 'public'
  } else if (['attribute_gte', 'attribute_lte', 'experience_gte'].includes(requirement.op)) {
    portalPolicyForm.mode = requirement.op as PortalPolicyMode
    portalPolicyForm.definitionCode = requirement.attribute_code ?? ''
    portalPolicyForm.value = (requirement.value_units ?? 0) / 1000
  } else if (requirement.op === 'role_active' || requirement.op === 'role_inactive') {
    portalPolicyForm.mode = requirement.op
    portalPolicyForm.definitionCode = requirement.role_code ?? ''
  } else if (requirement.op === 'status_present' || requirement.op === 'status_absent') {
    portalPolicyForm.mode = requirement.op
    portalPolicyForm.definitionCode = requirement.status_code ?? ''
    portalPolicyForm.minimumStacks = requirement.minimum_stacks ?? 0
  } else if (requirement.op === 'fact_count_gte') {
    portalPolicyForm.mode = requirement.op
    portalPolicyForm.factType = requirement.fact_type ?? ''
    portalPolicyForm.value = requirement.value_units ?? 0
    portalPolicyForm.windowTicks = requirement.window_ticks ?? 1
  } else if (requirement.op === 'world_tick_gte') {
    portalPolicyForm.mode = requirement.op
    portalPolicyForm.value = requirement.value_units ?? 0
  }
}

function configurePortalPolicy(): void {
  const portal = selectedPortalForPolicy.value
  const requirement = portalPolicyRequirement.value
  if (!props.systemAdmin || !portal || !requirement || !portalPolicyReady.value) return
  emit('command', 'portal.access.configure', {
    building_code: portal.state.building_code,
    portal_code: portal.state.portal_code,
    requirements: requirement
  }, portalPolicyCommandCode.value)
}

function memberInitial(member: CityMember): string {
  return (member.username || member.email || String(member.user_id)).trim().slice(0, 1).toUpperCase()
}

function memberDisplayName(userID: number): string {
  const member = props.members.find(item => item.user_id === userID)
  if (!member) return `#${userID}`
  return member.username ? `${member.username} · #${userID}` : `${member.email} · #${userID}`
}

function memberRoleLabel(role: CityMemberRole): string {
  const key = `citySpatial.runtime.members.roles.${role}`
  return te(key) ? t(key) : prettifyCode(role)
}

function addMember(): void {
  if (!props.systemAdmin || !memberForm.identity || props.memberBusyKey) return
  emit('memberAdd', { identity: memberForm.identity, role: memberForm.role })
}

function updateMemberRole(userID: number, value: string | number | boolean | null): void {
  if (!props.systemAdmin || typeof value !== 'string' || !['planner', 'treasurer', 'trader', 'viewer'].includes(value)) return
  emit('memberUpdate', userID, { role: value as Exclude<CityMemberRole, 'owner'> })
}

function removeMember(userID: number): void {
  if (!props.systemAdmin) return
  emit('memberUpdate', userID, { status: 'left' })
}

function archetypeIndex(code: string): string {
  const index = archetypes.value.findIndex(item => item.code === code)
  return String(index + 1).padStart(2, '0')
}

function definitionInitialAttributes(definition: WorldRuntimeDefinition): Record<string, number> {
  const value = payloadRecord(definition).initial_attributes
  return value && typeof value === 'object' ? value as Record<string, number> : {}
}

function attributeDefinition(code: string): Record<string, unknown> {
  const definition = definitions.value.find(item => item.kind === 'attribute' && item.code === code)
  return definition ? payloadRecord(definition) : {}
}

function attributePercent(attribute: WorldActorAttribute): number {
  const definition = attributeDefinition(attribute.attribute_code)
  const minimum = Number(definition.minimum_units ?? 0)
  const maximum = Number(definition.maximum_units ?? 100000)
  if (!Number.isFinite(minimum) || !Number.isFinite(maximum) || maximum <= minimum) return 0
  return Math.max(0, Math.min(100, ((attribute.value_units - minimum) / (maximum - minimum)) * 100))
}

function formatScaled(value: number): string {
  return new Intl.NumberFormat(undefined, { maximumFractionDigits: 1 }).format(value / 1000)
}

function activityEffectSummary(definition: WorldRuntimeDefinition): string {
  const effects = payloadRecord(definition).effects
  if (!Array.isArray(effects)) return ''
  return effects.map(effect => {
    const item = effect as Record<string, unknown>
    const value = Number(item.value_units ?? item.stacks ?? 0)
    const sign = value > 0 ? '+' : ''
    return `${definitionLabel(String(item.type ?? '').startsWith('status') ? 'status' : 'attribute', String(item.key ?? ''))} ${sign}${formatScaled(value)}`
  }).join(' · ')
}

function roleBlockSummary(option: WorldActorRoleOption): string {
  if (option.cooldown_remaining_ticks > 0) {
    return t('citySpatial.runtime.roleState.cooldown', { count: option.cooldown_remaining_ticks })
  }
  const failure = option.evaluation.failures[0]
  if (failure) {
    const code = failure.code ? definitionLabel(
      failure.operator.startsWith('role_') ? 'role' : failure.operator.startsWith('status_') ? 'status' : 'attribute',
      failure.code
    ) : t('citySpatial.runtime.roleState.requirements')
    return failure.required_units === undefined
      ? code
      : `${code} ${formatScaled(failure.actual_units ?? 0)} / ${formatScaled(failure.required_units)}`
  }
  return t('citySpatial.runtime.roleState.requirements')
}

function ruleScope(definition: WorldRuntimeDefinition): string {
  const payload = payloadRecord(definition)
  return `${String(payload.category_code ?? 'rule').toUpperCase()} · ${String(payload.scope_kind ?? 'world').toUpperCase()}:${String(payload.scope_code ?? 'world')}`
}

function factTypeLabel(value: string): string {
  const key = `worldRuntime.facts.${value.split('.').join('_')}`
  return te(key) ? t(key) : prettifyCode(value)
}

function createActor(): void {
  if (!createForm.name || !createForm.archetypeCode) return
  emit('command', 'actor.create', {
    name: createForm.name,
    archetype_code: createForm.archetypeCode
  }, 'actor.create')
}

function performActivity(activityCode: string): void {
  if (!props.selectedActorCode) return
  emit('command', 'actor.activity.perform', {
    actor_code: props.selectedActorCode,
    activity_code: activityCode
  }, `activity:${activityCode}`)
}

function transitionRole(roleCode: string): void {
  if (!props.selectedActorCode) return
  emit('command', 'actor.role.transition', {
    actor_code: props.selectedActorCode,
    role_code: roleCode
  }, `role:${roleCode}`)
}

function moveActor(dx: number, dy: number, dz: number, direction: string): void {
  const location = actorLocation.value
  if (!props.selectedActorCode || !location || !canCommand.value) return
  emit('command', 'actor.location.move', {
    actor_code: props.selectedActorCode,
    x: location.x + dx,
    y: location.y + dy,
    z: location.z + dz
  }, `move:${direction}`)
}

function moveToNextNavigationStep(): void {
  const step = nextNavigationStep.value
  if (!props.selectedActorCode || !step || !canCommand.value) return
  emit('command', 'actor.location.move', {
    actor_code: props.selectedActorCode,
    x: step.coordinate.x,
    y: step.coordinate.y,
    z: step.coordinate.z
  }, 'move:navigation')
}

function setNavigationIntent(): void {
  if (!props.selectedActorCode || !navigationIntentReady.value) return
  emit('command', 'actor.navigation.intent.set', {
    actor_code: props.selectedActorCode,
    destination: {
      x: navigationIntentForm.x,
      y: navigationIntentForm.y,
      z: navigationIntentForm.z
    },
    priority: navigationIntentForm.priority,
    max_steps: navigationIntentForm.maxSteps,
    on_blocked: navigationIntentForm.onBlocked
  }, navigationIntentSetCommandCode.value)
}

function cancelNavigationIntent(): void {
  if (!props.selectedActorCode || !canCommand.value || !navigationIntentCancellable.value) return
  emit('command', 'actor.navigation.intent.cancel', {
    actor_code: props.selectedActorCode
  }, navigationIntentCancelCommandCode.value)
}

function selectedControlCapabilities(): WorldActorCapability[] {
  const capabilities: WorldActorCapability[] = []
  if (controlForm.command) capabilities.push('actor.command')
  if (controlForm.manage) capabilities.push('actor.control.manage')
  return capabilities
}

function grantControl(): void {
  if (!props.selectedActorCode || !canManageControl.value || !controlGrantReady.value) return
  const userID = Number(controlForm.userID)
  emit('command', 'actor.control.grant', {
    actor_code: props.selectedActorCode,
    user_id: userID,
    capabilities: selectedControlCapabilities()
  }, `control:grant:${userID}`)
}

function revokeControl(userID: number, capability: WorldActorCapability): void {
  if (!props.selectedActorCode || !canManageControl.value) return
  emit('command', 'actor.control.revoke', {
    actor_code: props.selectedActorCode,
    user_id: userID,
    capabilities: [capability]
  }, `control:revoke:${userID}:${capability}`)
}

function capabilityLabel(capability: WorldActorCapability): string {
  return capability === 'actor.command'
    ? t('citySpatial.runtime.control.actorCommand')
    : t('citySpatial.runtime.control.manageControl')
}
</script>

<style scoped>
.world-runtime-panel { position: relative; margin-top: 1rem; border: 1px solid var(--ui-separator); background: var(--ui-surface); }
.runtime-header { display: flex; align-items: flex-end; justify-content: space-between; gap: 1.5rem; border-bottom: 1px solid var(--ui-separator); padding: 1rem; }
.runtime-header p, .runtime-section-heading p { margin: 0; color: var(--ui-label-secondary); font: 0.6rem ui-monospace, monospace; letter-spacing: 0.13em; text-transform: uppercase; }
.runtime-header h2 { margin: 0.2rem 0; font-size: 1rem; }
.runtime-header > div > span { color: var(--ui-label-secondary); font-size: 0.73rem; }
.runtime-counters { display: flex; margin: 0; border: 1px solid var(--ui-separator); }
.runtime-counters div { min-width: 4.5rem; border-right: 1px solid var(--ui-separator); padding: 0.45rem 0.65rem; }
.runtime-counters div:last-child { border-right: 0; }
.runtime-counters dt { color: var(--ui-label-secondary); font: 0.55rem ui-monospace, monospace; text-transform: uppercase; }
.runtime-counters dd { margin: 0.15rem 0 0; font: 0.75rem ui-monospace, monospace; }
.runtime-unavailable, .runtime-empty { margin: 0; padding: 1rem; color: var(--ui-label-secondary); font-size: 0.72rem; }
.runtime-membership, .runtime-receipts { border-bottom: 1px solid var(--ui-separator); }
.runtime-member-add { display: grid; grid-template-columns: minmax(14rem, 1fr) minmax(9rem, 0.45fr) auto; align-items: end; gap: 0.65rem; border-top: 1px solid var(--ui-separator); padding: 0.75rem 0.8rem; }
.runtime-member-add > label > span { display: block; margin-bottom: 0.28rem; color: var(--ui-label-secondary); font-size: 0.6rem; }
.runtime-member-list { display: grid; grid-template-columns: repeat(auto-fit, minmax(22rem, 1fr)); max-height: 15rem; overflow: auto; border-top: 1px solid var(--ui-separator); }
.runtime-member-list article { display: grid; min-width: 0; grid-template-columns: minmax(11rem, 1fr) minmax(8rem, 0.55fr) auto; align-items: center; gap: 0.6rem; border-right: 1px solid var(--ui-separator); border-bottom: 1px solid var(--ui-separator); padding: 0.55rem 0.7rem; }
.runtime-member-identity { display: flex; min-width: 0; align-items: center; gap: 0.55rem; }
.runtime-member-identity > span { display: grid; width: 1.75rem; height: 1.75rem; flex: none; place-items: center; background: var(--ui-control); color: var(--ui-accent); font: 0.7rem ui-monospace, monospace; }
.runtime-member-identity > div { min-width: 0; }
.runtime-member-identity strong, .runtime-member-identity small { display: block; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.runtime-member-identity strong { font-size: 0.68rem; }
.runtime-member-identity small { margin-top: 0.12rem; color: var(--ui-label-secondary); font: 0.54rem ui-monospace, monospace; }
.runtime-member-role { color: var(--ui-label-secondary); font: 0.58rem ui-monospace, monospace; text-transform: uppercase; }
.runtime-member-remove { color: var(--ui-danger, #dc2626); font-size: 0.6rem; }
.runtime-member-remove:disabled { opacity: 0.45; }
.runtime-receipt-list { display: flex; overflow-x: auto; border-top: 1px solid var(--ui-separator); }
.runtime-receipt-list article { display: grid; min-width: 13rem; grid-template-columns: auto 1fr; gap: 0.14rem 0.5rem; border-right: 1px solid var(--ui-separator); padding: 0.58rem 0.7rem; box-shadow: inset 0 2px 0 var(--ui-label-secondary); }
.runtime-receipt-list article[data-status='applied'] { box-shadow: inset 0 2px 0 var(--ui-success, #16a34a); }
.runtime-receipt-list article[data-status='rejected'] { box-shadow: inset 0 2px 0 var(--ui-danger, #dc2626); }
.runtime-receipt-sequence { grid-row: 1 / 3; color: var(--ui-label-secondary); font: 0.57rem ui-monospace, monospace; }
.runtime-receipt-list strong { overflow: hidden; font-size: 0.64rem; text-overflow: ellipsis; white-space: nowrap; }
.runtime-receipt-status { font: 0.55rem ui-monospace, monospace; text-transform: uppercase; }
.runtime-receipt-list article[data-status='applied'] .runtime-receipt-status { color: var(--ui-success, #16a34a); }
.runtime-receipt-list article[data-status='rejected'] .runtime-receipt-status { color: var(--ui-danger, #dc2626); }
.runtime-receipt-list small { grid-column: 2; color: var(--ui-label-secondary); font: 0.53rem ui-monospace, monospace; }
.runtime-actor-tabs { display: flex; overflow-x: auto; border-bottom: 1px solid var(--ui-separator); }
.runtime-actor-tabs button { display: grid; min-width: 13rem; grid-template-columns: 2rem 1fr; column-gap: 0.65rem; border-right: 1px solid var(--ui-separator); padding: 0.65rem 0.8rem; text-align: left; }
.runtime-actor-tabs button > span { display: grid; grid-row: 1 / 3; place-items: center; border: 1px solid var(--ui-separator); color: var(--ui-accent); font: 0.72rem ui-monospace, monospace; }
.runtime-actor-tabs strong { font-size: 0.75rem; }
.runtime-actor-tabs small { color: var(--ui-label-secondary); font: 0.57rem ui-monospace, monospace; }
.runtime-actor-tabs button.active { box-shadow: inset 0 -2px 0 var(--ui-accent); background: var(--ui-control); }
.runtime-section-heading { display: flex; align-items: flex-end; justify-content: space-between; gap: 1rem; border-bottom: 1px solid var(--ui-separator); padding: 0.7rem 0.8rem; }
.runtime-section-heading h3 { margin: 0.12rem 0 0; font-size: 0.82rem; }
.runtime-section-heading > span { color: var(--ui-label-secondary); font: 0.6rem ui-monospace, monospace; }
.runtime-character-create { padding: 1rem; }
.runtime-character-create > .runtime-section-heading { margin: -1rem -1rem 1rem; }
.runtime-archetypes { display: grid; grid-template-columns: repeat(auto-fit, minmax(15rem, 1fr)); border-top: 1px solid var(--ui-separator); border-left: 1px solid var(--ui-separator); }
.runtime-archetypes label { position: relative; display: grid; gap: 0.35rem; border-right: 1px solid var(--ui-separator); border-bottom: 1px solid var(--ui-separator); padding: 0.85rem; cursor: pointer; }
.runtime-archetypes label.active { box-shadow: inset 3px 0 0 var(--ui-accent); background: var(--ui-control); }
.runtime-archetypes input { position: absolute; opacity: 0; pointer-events: none; }
.runtime-archetype-index { color: var(--ui-accent); font: 0.6rem ui-monospace, monospace; }
.runtime-archetypes strong { font-size: 0.8rem; }
.runtime-archetypes small { min-height: 2.6rem; color: var(--ui-label-secondary); font-size: 0.68rem; line-height: 1.45; }
.runtime-archetypes dl { display: grid; grid-template-columns: repeat(2, 1fr); gap: 0.25rem 0.7rem; margin: 0.5rem 0 0; }
.runtime-archetypes dl div { display: flex; justify-content: space-between; gap: 0.5rem; color: var(--ui-label-secondary); font: 0.58rem ui-monospace, monospace; }
.runtime-archetypes dd { margin: 0; color: var(--ui-label); }
.runtime-create-controls { display: grid; grid-template-columns: minmax(14rem, 1fr) auto; align-items: end; gap: 0.75rem; margin-top: 0.8rem; }
.runtime-create-controls label > span { display: block; margin-bottom: 0.3rem; color: var(--ui-label-secondary); font-size: 0.68rem; }
.runtime-portals { border-top: 1px solid var(--ui-separator); border-bottom: 1px solid var(--ui-separator); }
.runtime-portal-list { display: grid; grid-template-columns: repeat(2, minmax(0, 1fr)); max-height: 34rem; overflow: auto; border-left: 1px solid var(--ui-separator); }
.runtime-portal-list > article { min-width: 0; border-right: 1px solid var(--ui-separator); border-bottom: 1px solid var(--ui-separator); background: var(--ui-surface); box-shadow: inset 3px 0 0 var(--ui-success, #16a34a); }
.runtime-portal-list > article[data-state='closed'] { box-shadow: inset 3px 0 0 var(--ui-warning, #d97706); }
.runtime-portal-list > article[data-state='locked'] { box-shadow: inset 3px 0 0 var(--ui-danger, #dc2626); }
.runtime-portal-list article > header { display: flex; align-items: flex-start; justify-content: space-between; gap: 0.7rem; border-bottom: 1px solid var(--ui-separator); padding: 0.65rem 0.75rem 0.6rem 0.9rem; }
.runtime-portal-list article > header > div:first-child { min-width: 0; }
.runtime-portal-list article > header span, .runtime-portal-policy span { display: block; color: var(--ui-label-secondary); font: 0.52rem ui-monospace, monospace; text-transform: uppercase; }
.runtime-portal-list article > header strong { display: block; margin-top: 0.16rem; overflow: hidden; font: 0.7rem ui-monospace, monospace; text-overflow: ellipsis; white-space: nowrap; }
.runtime-portal-badges { display: flex; flex: none; flex-wrap: wrap; justify-content: flex-end; gap: 0.25rem; }
.runtime-portal-badges span { border: 1px solid var(--ui-separator); padding: 0.2rem 0.35rem; color: var(--ui-label-secondary); }
.runtime-portal-badges span[data-state='open'] { border-color: var(--ui-success, #16a34a); color: var(--ui-success, #16a34a); }
.runtime-portal-badges span[data-state='closed'] { border-color: var(--ui-warning, #d97706); color: var(--ui-warning, #d97706); }
.runtime-portal-badges span[data-state='locked'] { border-color: var(--ui-danger, #dc2626); color: var(--ui-danger, #dc2626); }
.runtime-portal-route { display: grid; grid-template-columns: repeat(4, minmax(0, 1fr)); margin: 0; border-bottom: 1px solid var(--ui-separator); }
.runtime-portal-route div { min-width: 0; border-right: 1px solid var(--ui-separator); padding: 0.45rem 0.55rem; }
.runtime-portal-route div:last-child { border-right: 0; }
.runtime-portal-route dt { color: var(--ui-label-secondary); font: 0.5rem ui-monospace, monospace; text-transform: uppercase; }
.runtime-portal-route dd { margin: 0.15rem 0 0; overflow: hidden; font: 0.58rem ui-monospace, monospace; text-overflow: ellipsis; white-space: nowrap; }
.runtime-portal-policy { display: grid; grid-template-columns: minmax(0, 1.4fr) minmax(8rem, 0.6fr); gap: 0.75rem; padding: 0.55rem 0.75rem 0.55rem 0.9rem; }
.runtime-portal-policy > div { min-width: 0; }
.runtime-portal-policy strong { display: block; margin-top: 0.18rem; overflow-wrap: anywhere; font-size: 0.62rem; line-height: 1.4; }
.runtime-portal-policy strong[data-access='allowed'] { color: var(--ui-success, #16a34a); }
.runtime-portal-policy strong[data-access='denied'], .runtime-portal-policy strong[data-access='locked'] { color: var(--ui-danger, #dc2626); }
.runtime-portal-policy strong[data-access='closed'] { color: var(--ui-warning, #d97706); }
.runtime-portal-failure { margin: 0; border-top: 1px solid var(--ui-separator); padding: 0.45rem 0.75rem 0.45rem 0.9rem; color: var(--ui-danger, #dc2626); font-size: 0.56rem; }
.runtime-portal-list article > footer { display: flex; min-height: 2.75rem; align-items: center; justify-content: space-between; gap: 0.75rem; border-top: 1px solid var(--ui-separator); padding: 0.45rem 0.75rem 0.45rem 0.9rem; }
.runtime-portal-actions { display: flex; min-width: 0; flex-wrap: wrap; align-items: center; gap: 0.35rem; }
.runtime-portal-actions > span { color: var(--ui-label-secondary); font-size: 0.55rem; }
.runtime-portal-policy-form { display: grid; grid-template-columns: minmax(13rem, 1.4fr) repeat(4, minmax(9rem, 1fr)) auto; align-items: end; gap: 0.6rem; border-top: 1px solid var(--ui-separator); padding: 0.75rem 0.8rem; background: var(--ui-canvas-raised); }
.runtime-portal-policy-heading { align-self: center; }
.runtime-portal-policy-heading > span { color: var(--ui-accent); font: 0.52rem ui-monospace, monospace; letter-spacing: 0.12em; }
.runtime-portal-policy-heading > strong, .runtime-portal-policy-heading > small { display: block; }
.runtime-portal-policy-heading > strong { margin-top: 0.12rem; font-size: 0.72rem; }
.runtime-portal-policy-heading > small { margin-top: 0.18rem; color: var(--ui-label-secondary); font-size: 0.55rem; line-height: 1.35; }
.runtime-portal-policy-form > label { min-width: 0; }
.runtime-portal-policy-form > label > span { display: block; margin-bottom: 0.25rem; color: var(--ui-label-secondary); font-size: 0.56rem; }
.runtime-actor-workbench { display: grid; }
.runtime-identity-card { display: grid; grid-template-columns: 3.5rem minmax(0, 1fr) auto; align-items: center; gap: 0.9rem; border-bottom: 1px solid var(--ui-separator); padding: 0.9rem 1rem; background: var(--ui-canvas-raised); }
.runtime-avatar { display: grid; width: 3.5rem; height: 3.5rem; place-items: center; border: 1px solid var(--ui-separator); color: var(--ui-accent); font: 0.9rem ui-monospace, monospace; }
.runtime-identity-card p { margin: 0; color: var(--ui-label-secondary); font-size: 0.65rem; }
.runtime-identity-card h3 { margin: 0.15rem 0; font-size: 1rem; }
.runtime-identity-card > div > span { color: var(--ui-label-secondary); font: 0.58rem ui-monospace, monospace; }
.runtime-active-roles { display: flex; flex-wrap: wrap; justify-content: flex-end; gap: 0.35rem; }
.runtime-active-roles span, .runtime-active-mark { border: 1px solid var(--ui-separator); padding: 0.25rem 0.4rem; color: var(--ui-accent); font: 0.58rem ui-monospace, monospace; text-transform: uppercase; }
.runtime-spatial-control { display: grid; grid-template-columns: minmax(0, 1.15fr) minmax(0, 0.85fr); border-bottom: 1px solid var(--ui-separator); }
.runtime-spatial-control > article { min-width: 0; }
.runtime-spatial-control > article:first-child { border-right: 1px solid var(--ui-separator); }
.runtime-text-action { color: var(--ui-accent); font: 0.6rem ui-monospace, monospace; }
.runtime-text-action:hover, .runtime-text-action:focus-visible { text-decoration: underline; }
.runtime-location-facts { display: grid; grid-template-columns: repeat(3, minmax(0, 1fr)); margin: 0; }
.runtime-location-facts div { min-width: 0; border-right: 1px solid var(--ui-separator); border-bottom: 1px solid var(--ui-separator); padding: 0.55rem 0.65rem; }
.runtime-location-facts div:nth-child(3n) { border-right: 0; }
.runtime-location-facts dt { color: var(--ui-label-secondary); font: 0.55rem ui-monospace, monospace; text-transform: uppercase; }
.runtime-location-facts dd { margin: 0.2rem 0 0; overflow-wrap: anywhere; font: 0.64rem ui-monospace, monospace; }
.runtime-movement-console { display: grid; grid-template-columns: 8.5rem minmax(0, 1fr); align-items: center; gap: 1rem; padding: 0.8rem; }
.runtime-movement-pad { display: grid; grid-template: repeat(3, 2.5rem) / repeat(3, 2.5rem); gap: 1px; background: var(--ui-separator); border: 1px solid var(--ui-separator); }
.runtime-movement-pad button, .runtime-move-center { display: grid; place-items: center; background: var(--ui-surface); font: 0.8rem ui-monospace, monospace; }
.runtime-movement-pad button:hover:not(:disabled), .runtime-movement-pad button:focus-visible { background: var(--ui-control); color: var(--ui-accent); }
.runtime-movement-pad button:disabled { color: var(--ui-label-secondary); opacity: 0.55; }
.runtime-move-northWest { grid-area: 1 / 1; }
.runtime-move-north { grid-area: 1 / 2; }
.runtime-move-northEast { grid-area: 1 / 3; }
.runtime-move-west { grid-area: 2 / 1; }
.runtime-move-center { grid-area: 2 / 2; color: var(--ui-accent); }
.runtime-move-east { grid-area: 2 / 3; }
.runtime-move-southWest { grid-area: 3 / 1; }
.runtime-move-south { grid-area: 3 / 2; }
.runtime-move-southEast { grid-area: 3 / 3; }
.runtime-vertical-movement { display: grid; grid-template-columns: 1fr 1fr; gap: 0.4rem; }
.runtime-vertical-movement button { display: flex; align-items: center; justify-content: center; gap: 0.35rem; border: 1px solid var(--ui-separator); padding: 0.55rem; font-size: 0.65rem; }
.runtime-vertical-movement button:hover:not(:disabled), .runtime-vertical-movement button:focus-visible { background: var(--ui-control); color: var(--ui-accent); }
.runtime-vertical-movement button span { font: 0.8rem ui-monospace, monospace; }
.runtime-vertical-movement small { grid-column: 1 / -1; color: var(--ui-label-secondary); font-size: 0.6rem; line-height: 1.45; }
.runtime-navigation-console { border-top: 1px solid var(--ui-separator); padding: 0.8rem; }
.runtime-navigation-heading { display: flex; align-items: center; justify-content: space-between; gap: 1rem; }
.runtime-navigation-heading > div { display: grid; gap: 0.15rem; }
.runtime-navigation-heading span { color: var(--ui-label-secondary); font: 0.53rem ui-monospace, monospace; letter-spacing: 0.12em; }
.runtime-navigation-heading strong { font-size: 0.72rem; }
.runtime-navigation-heading code { padding: 0.25rem 0.4rem; background: var(--ui-control); color: var(--ui-label-secondary); font-size: 0.62rem; }
.runtime-navigation-actions { display: flex; align-items: center; gap: 0.65rem; margin-top: 0.65rem; }
.runtime-navigation-hint, .runtime-navigation-error { margin: 0.6rem 0 0; font-size: 0.62rem; line-height: 1.5; }
.runtime-navigation-hint { color: var(--ui-label-secondary); }
.runtime-navigation-error { color: var(--ui-danger, #dc2626); }
.runtime-navigation-result { display: flex; align-items: center; justify-content: space-between; gap: 0.75rem; margin-top: 0.65rem; border: 1px solid var(--ui-separator); padding: 0.55rem; }
.runtime-navigation-result[data-reachable="false"] { border-left: 3px solid var(--ui-danger, #dc2626); }
.runtime-navigation-result dl { display: flex; flex-wrap: wrap; gap: 0.8rem; margin: 0; }
.runtime-navigation-result dl div { display: grid; gap: 0.12rem; }
.runtime-navigation-result dt { color: var(--ui-label-secondary); font-size: 0.52rem; text-transform: uppercase; }
.runtime-navigation-result dd { margin: 0; font: 0.65rem ui-monospace, monospace; }
.runtime-navigation-result button span { margin-left: 0.35rem; font: 0.55rem ui-monospace, monospace; opacity: 0.72; }
.runtime-navigation-steps { display: flex; gap: 1px; margin: 0.55rem 0 0; padding: 0; overflow-x: auto; background: var(--ui-separator); list-style: none; }
.runtime-navigation-steps li { display: grid; min-width: 5.8rem; grid-template-columns: auto 1fr; gap: 0.12rem 0.35rem; padding: 0.35rem 0.45rem; background: var(--ui-control); }
.runtime-navigation-steps li > span { color: var(--ui-accent); font: 0.52rem ui-monospace, monospace; }
.runtime-navigation-steps code { font-size: 0.56rem; }
.runtime-navigation-steps small { grid-column: 2; color: var(--ui-label-secondary); font-size: 0.5rem; }
.runtime-navigation-steps .runtime-navigation-overflow { display: grid; min-width: 3rem; place-items: center; color: var(--ui-label-secondary); }
.runtime-navigation-intent { margin-top: 0.8rem; border: 1px solid var(--ui-separator); background: var(--ui-surface); }
.runtime-navigation-intent > header { display: flex; min-height: 3.2rem; align-items: center; justify-content: space-between; gap: 1rem; border-bottom: 1px solid var(--ui-separator); padding: 0.55rem 0.65rem; }
.runtime-navigation-intent > header > div { display: grid; gap: 0.12rem; }
.runtime-navigation-intent > header > div span { color: var(--ui-accent); font: 0.5rem ui-monospace, monospace; letter-spacing: 0.1em; }
.runtime-navigation-intent > header strong { font-size: 0.7rem; }
.runtime-navigation-intent > header > span { color: var(--ui-label-secondary); font: 0.54rem ui-monospace, monospace; }
.runtime-navigation-intent-error, .runtime-navigation-intent-empty { margin: 0; border-bottom: 1px solid var(--ui-separator); padding: 0.55rem 0.65rem; font-size: 0.6rem; line-height: 1.45; }
.runtime-navigation-intent-error { color: var(--ui-danger, #dc2626); }
.runtime-navigation-intent-empty { color: var(--ui-label-secondary); }
.runtime-navigation-intent-state { box-shadow: inset 3px 0 0 var(--ui-accent); }
.runtime-navigation-intent-state[data-status='blocked'] { box-shadow: inset 3px 0 0 var(--ui-warning, #d97706); }
.runtime-navigation-intent-state[data-status='failed'], .runtime-navigation-intent-state[data-status='cancelled'] { box-shadow: inset 3px 0 0 var(--ui-danger, #dc2626); }
.runtime-navigation-intent-state[data-status='arrived'] { box-shadow: inset 3px 0 0 var(--ui-success, #16a34a); }
.runtime-navigation-intent-state-heading { display: flex; align-items: center; justify-content: space-between; gap: 0.75rem; border-bottom: 1px solid var(--ui-separator); padding: 0.55rem 0.65rem 0.5rem 0.8rem; }
.runtime-navigation-intent-state-heading > div { min-width: 0; }
.runtime-navigation-intent-state-heading span, .runtime-navigation-intent-state-heading strong { display: block; }
.runtime-navigation-intent-state-heading span { overflow: hidden; color: var(--ui-label-secondary); font: 0.5rem ui-monospace, monospace; text-overflow: ellipsis; white-space: nowrap; }
.runtime-navigation-intent-state-heading strong { margin-top: 0.12rem; font-size: 0.68rem; }
.runtime-navigation-intent-state-heading code { flex: none; color: var(--ui-label-secondary); font-size: 0.52rem; }
.runtime-navigation-intent-state dl { display: grid; grid-template-columns: repeat(3, minmax(0, 1fr)); margin: 0; }
.runtime-navigation-intent-state dl div { min-width: 0; border-right: 1px solid var(--ui-separator); border-bottom: 1px solid var(--ui-separator); padding: 0.45rem 0.55rem; }
.runtime-navigation-intent-state dl div:nth-child(3n) { border-right: 0; }
.runtime-navigation-intent-state dt { color: var(--ui-label-secondary); font: 0.5rem ui-monospace, monospace; }
.runtime-navigation-intent-state dd { margin: 0.15rem 0 0; overflow: hidden; font: 0.58rem ui-monospace, monospace; text-overflow: ellipsis; white-space: nowrap; }
.runtime-navigation-budget { display: grid; grid-template-columns: minmax(11rem, auto) minmax(8rem, 1fr); align-items: center; gap: 0.8rem; border-bottom: 1px solid var(--ui-separator); padding: 0.5rem 0.65rem 0.5rem 0.8rem; }
.runtime-navigation-budget > div:first-child { display: grid; grid-template-columns: auto auto; gap: 0.1rem 0.6rem; align-items: baseline; }
.runtime-navigation-budget span { color: var(--ui-label-secondary); font-size: 0.54rem; }
.runtime-navigation-budget strong { font: 0.62rem ui-monospace, monospace; }
.runtime-navigation-budget small { grid-column: 1 / -1; color: var(--ui-label-secondary); font: 0.48rem ui-monospace, monospace; }
.runtime-navigation-budget-meter { height: 0.3rem; background: var(--ui-control); }
.runtime-navigation-budget-meter i { display: block; height: 100%; background: var(--ui-accent); }
.runtime-navigation-intent-reason { margin: 0; border-bottom: 1px solid var(--ui-separator); padding: 0.45rem 0.65rem 0.45rem 0.8rem; color: var(--ui-warning, #d97706); font-size: 0.56rem; }
.runtime-navigation-intent-form { display: grid; grid-template-columns: minmax(12rem, 1.4fr) repeat(2, minmax(5.5rem, 0.55fr)) minmax(8rem, 0.8fr) auto; align-items: end; gap: 0.55rem; padding: 0.65rem; }
.runtime-navigation-intent-form > label > span, .runtime-navigation-coordinate-fields label > span { display: block; margin-bottom: 0.22rem; color: var(--ui-label-secondary); font-size: 0.52rem; }
.runtime-navigation-coordinate-fields { display: grid; grid-template-columns: repeat(3, minmax(0, 1fr)); gap: 0.35rem; }
.runtime-navigation-intent-form-actions { display: flex; grid-column: 1 / -1; justify-content: flex-end; gap: 0.4rem; border-top: 1px solid var(--ui-separator); padding-top: 0.55rem; }
.runtime-navigation-reservation { display: grid; grid-template-columns: auto minmax(0, 1fr) auto; align-items: center; gap: 0.65rem; border-top: 1px solid var(--ui-separator); padding: 0.5rem 0.65rem; }
.runtime-navigation-reservation span, .runtime-navigation-reservation small { color: var(--ui-label-secondary); font: 0.52rem ui-monospace, monospace; }
.runtime-navigation-reservation strong { overflow: hidden; font: 0.57rem ui-monospace, monospace; text-overflow: ellipsis; white-space: nowrap; }
.runtime-control-form { display: grid; grid-template-columns: minmax(7rem, 0.8fr) minmax(12rem, 1.2fr) auto; align-items: end; gap: 0.55rem; border-bottom: 1px solid var(--ui-separator); padding: 0.7rem 0.8rem; }
.runtime-control-form > label > span, .runtime-control-form legend { display: block; margin-bottom: 0.25rem; color: var(--ui-label-secondary); font-size: 0.6rem; }
.runtime-control-form fieldset { display: flex; min-width: 0; flex-wrap: wrap; gap: 0.4rem 0.7rem; margin: 0; border: 0; padding: 0; }
.runtime-control-form fieldset label { display: flex; align-items: center; gap: 0.3rem; font-size: 0.62rem; }
.runtime-control-form input[type='checkbox'] { width: 0.9rem; height: 0.9rem; accent-color: var(--ui-accent); }
.runtime-control-explanation { margin: 0; border-bottom: 1px solid var(--ui-separator); padding: 0.65rem 0.8rem; color: var(--ui-label-secondary); font-size: 0.62rem; }
.runtime-delegation-list { max-height: 12rem; overflow: auto; }
.runtime-delegation-list > article { display: grid; grid-template-columns: 4.5rem minmax(0, 1fr); align-items: center; gap: 0.65rem; border-bottom: 1px solid var(--ui-separator); padding: 0.55rem 0.8rem; }
.runtime-delegation-list strong, .runtime-delegation-list small { display: block; }
.runtime-delegation-list strong { font: 0.66rem ui-monospace, monospace; }
.runtime-delegation-list small { margin-top: 0.1rem; color: var(--ui-label-secondary); font-size: 0.56rem; }
.runtime-capability-list { display: flex; min-width: 0; flex-wrap: wrap; justify-content: flex-end; gap: 0.35rem; }
.runtime-capability-list > span { display: inline-flex; align-items: center; gap: 0.3rem; border: 1px solid var(--ui-separator); padding: 0.22rem 0.35rem; color: var(--ui-label-secondary); font: 0.55rem ui-monospace, monospace; }
.runtime-capability-list button { color: #d35b5b; font: 0.75rem/1 ui-monospace, monospace; }
.runtime-attribute-grid { display: grid; grid-template-columns: repeat(auto-fit, minmax(12rem, 1fr)); }
.runtime-attribute-grid article { border-right: 1px solid var(--ui-separator); border-bottom: 1px solid var(--ui-separator); padding: 0.75rem 0.8rem; }
.runtime-attribute-grid header, .runtime-attribute-grid footer { display: flex; justify-content: space-between; gap: 0.5rem; }
.runtime-attribute-grid header { font-size: 0.7rem; }
.runtime-attribute-grid header span { font: 0.72rem ui-monospace, monospace; }
.runtime-meter { height: 0.28rem; margin: 0.55rem 0; background: var(--ui-control); }
.runtime-meter i { display: block; height: 100%; background: var(--ui-accent); }
.runtime-attribute-grid footer { color: var(--ui-label-secondary); font: 0.55rem ui-monospace, monospace; }
.runtime-actions-grid { display: grid; grid-template-columns: 1fr 1fr; border-top: 1px solid var(--ui-separator); }
.runtime-actions-grid > section:first-child { border-right: 1px solid var(--ui-separator); }
.runtime-activity-list, .runtime-role-list { display: grid; max-height: 24rem; overflow: auto; }
.runtime-activity-list article, .runtime-role-list article { display: flex; align-items: center; justify-content: space-between; gap: 1rem; border-bottom: 1px solid var(--ui-separator); padding: 0.75rem 0.8rem; }
.runtime-activity-list article > div, .runtime-role-list article > div { min-width: 0; }
.runtime-activity-list strong, .runtime-role-list strong { font-size: 0.73rem; }
.runtime-activity-list p, .runtime-role-list p, .runtime-rule-list p { margin: 0.18rem 0; color: var(--ui-label-secondary); font-size: 0.65rem; line-height: 1.45; }
.runtime-activity-list small, .runtime-role-list small { color: var(--ui-label-secondary); font: 0.56rem ui-monospace, monospace; }
.runtime-role-list article.active { box-shadow: inset 3px 0 0 var(--ui-accent); }
.runtime-governance-grid { display: grid; grid-template-columns: repeat(3, minmax(0, 1fr)); border-top: 1px solid var(--ui-separator); }
.runtime-governance-grid > section { min-width: 0; border-right: 1px solid var(--ui-separator); }
.runtime-governance-grid > section:last-child { border-right: 0; }
.runtime-status-list, .runtime-case-list, .runtime-rule-list { max-height: 18rem; overflow: auto; }
.runtime-status-list article, .runtime-case-list article, .runtime-rule-list article { border-bottom: 1px solid var(--ui-separator); padding: 0.65rem 0.8rem; }
.runtime-status-list header { display: flex; justify-content: space-between; gap: 0.5rem; font-size: 0.7rem; }
.runtime-status-list header span { color: var(--ui-label-secondary); font: 0.55rem ui-monospace, monospace; text-transform: uppercase; }
.runtime-status-list article[data-lifecycle='active'] { box-shadow: inset 3px 0 0 #d97706; }
.runtime-status-list p { margin: 0.25rem 0; color: var(--ui-label-secondary); font-size: 0.63rem; }
.runtime-status-list footer { color: var(--ui-label-secondary); font: 0.55rem ui-monospace, monospace; }
.runtime-case-list article { display: grid; grid-template-columns: auto 1fr; gap: 0.2rem 0.6rem; }
.runtime-case-list span { grid-row: 1 / 3; color: #d97706; font: 0.58rem ui-monospace, monospace; }
.runtime-case-list strong { font-size: 0.68rem; }
.runtime-case-list small, .runtime-rule-list small { color: var(--ui-label-secondary); font: 0.55rem ui-monospace, monospace; }
.runtime-rule-list strong { font-size: 0.7rem; }
.runtime-fact-stream { border-top: 1px solid var(--ui-separator); }
.runtime-fact-stream > div:last-child { display: grid; grid-template-columns: repeat(auto-fit, minmax(14rem, 1fr)); max-height: 12rem; overflow: auto; }
.runtime-fact-stream article { display: grid; grid-template-columns: auto 1fr; gap: 0.15rem 0.55rem; border-right: 1px solid var(--ui-separator); border-bottom: 1px solid var(--ui-separator); padding: 0.6rem 0.75rem; }
.runtime-fact-stream article span { grid-row: 1 / 3; color: var(--ui-accent); font: 0.58rem ui-monospace, monospace; }
.runtime-fact-stream article strong { font-size: 0.68rem; }
.runtime-fact-stream article small { color: var(--ui-label-secondary); font: 0.55rem ui-monospace, monospace; }
.runtime-loading-line { position: absolute; top: 0; left: 0; width: 35%; height: 2px; background: var(--ui-accent); animation: runtime-loading 1s steps(8, end) infinite; }
@keyframes runtime-loading { from { transform: translateX(-100%); } to { transform: translateX(385%); } }
@media (max-width: 1000px) {
  .runtime-spatial-control, .runtime-actions-grid, .runtime-governance-grid { grid-template-columns: 1fr; }
  .runtime-portal-list { grid-template-columns: 1fr; }
  .runtime-portal-policy-form { grid-template-columns: repeat(2, minmax(0, 1fr)); }
  .runtime-portal-policy-heading { grid-column: 1 / -1; }
  .runtime-navigation-intent-form { grid-template-columns: repeat(2, minmax(0, 1fr)); }
  .runtime-navigation-coordinate-fields { grid-column: 1 / -1; }
  .runtime-spatial-control > article:first-child { border-right: 0; border-bottom: 1px solid var(--ui-separator); }
  .runtime-actions-grid > section:first-child, .runtime-governance-grid > section { border-right: 0; border-bottom: 1px solid var(--ui-separator); }
}
@media (max-width: 640px) {
  .runtime-header, .runtime-identity-card { align-items: flex-start; grid-template-columns: 1fr; flex-direction: column; }
  .runtime-counters { width: 100%; overflow-x: auto; }
  .runtime-create-controls, .runtime-member-add { grid-template-columns: 1fr; }
  .runtime-portal-policy-form { grid-template-columns: 1fr; }
  .runtime-portal-route { grid-template-columns: repeat(2, minmax(0, 1fr)); }
  .runtime-portal-route div:nth-child(2) { border-right: 0; }
  .runtime-portal-route div:nth-child(-n + 2) { border-bottom: 1px solid var(--ui-separator); }
  .runtime-portal-policy { grid-template-columns: 1fr; }
  .runtime-member-list { grid-template-columns: 1fr; }
  .runtime-member-list article { grid-template-columns: minmax(0, 1fr) auto; }
  .runtime-member-list article > :deep(.relative) { grid-column: 1; }
  .runtime-active-roles { justify-content: flex-start; }
  .runtime-location-facts { grid-template-columns: repeat(2, minmax(0, 1fr)); }
  .runtime-location-facts div:nth-child(3n) { border-right: 1px solid var(--ui-separator); }
  .runtime-location-facts div:nth-child(2n) { border-right: 0; }
  .runtime-movement-console, .runtime-control-form { grid-template-columns: 1fr; }
  .runtime-navigation-intent-state dl, .runtime-navigation-intent-form, .runtime-navigation-budget { grid-template-columns: 1fr; }
  .runtime-navigation-intent-state dl div, .runtime-navigation-intent-state dl div:nth-child(3n) { border-right: 0; }
  .runtime-navigation-reservation { grid-template-columns: 1fr; }
}
@media (prefers-reduced-motion: reduce) { .runtime-loading-line { animation: none; width: 100%; } }
</style>
