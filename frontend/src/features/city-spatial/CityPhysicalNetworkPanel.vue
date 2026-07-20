<template>
  <section class="city-network-panel" :aria-busy="loading">
    <div v-if="loading && catalog" class="city-network-progress" aria-hidden="true"><span /></div>

    <div v-if="availability === 'unknown' && !catalog" class="city-network-empty">
      <span aria-hidden="true">⌁</span>
      <p>{{ t('citySpatial.services.network.loading') }}</p>
    </div>

    <div v-else-if="availability === 'unsupported' || catalog?.availability === 'unsupported'" class="city-network-unsupported">
      <code>{{ catalog?.simulation_version ?? '—' }}</code>
      <div>
        <strong>{{ t('citySpatial.services.network.unsupported.title') }}</strong>
        <p>{{ t('citySpatial.services.network.unsupported.description', { version: catalog?.required_version ?? 'city-f8-v3' }) }}</p>
      </div>
    </div>

    <template v-else-if="catalog?.availability === 'available' && catalog.profile && catalog.overview">
      <div class="city-network-summary">
        <div>
          <span>{{ t('citySpatial.services.network.metrics.networks') }}</span>
          <strong>{{ formatInteger(catalog.overview.active_network_count) }}</strong>
          <small>{{ t('citySpatial.services.network.metrics.nodes', { count: catalog.overview.active_node_count }) }}</small>
        </div>
        <div>
          <span>{{ t('citySpatial.services.network.metrics.edges') }}</span>
          <strong>{{ formatInteger(catalog.overview.active_edge_count) }}</strong>
          <small :data-alert="catalog.overview.isolated_edge_count + catalog.overview.failed_edge_count > 0">
            {{ t('citySpatial.services.network.metrics.edgeExceptions', { isolated: catalog.overview.isolated_edge_count, failed: catalog.overview.failed_edge_count }) }}
          </small>
        </div>
        <div>
          <span>{{ t('citySpatial.services.network.metrics.capacity') }}</span>
          <strong>{{ formatInteger(catalog.overview.available_edge_capacity_units) }}</strong>
          <small>{{ t('citySpatial.services.network.metrics.installed', { value: formatInteger(catalog.overview.installed_edge_capacity_units) }) }}</small>
        </div>
        <div>
          <span>{{ t('citySpatial.services.network.metrics.dispatched') }}</span>
          <strong>{{ formatInteger(catalog.overview.latest_dispatched_units) }}</strong>
          <small>{{ latestFlowTick }}</small>
        </div>
        <div>
          <span>{{ t('citySpatial.services.network.metrics.received') }}</span>
          <strong>{{ formatInteger(catalog.overview.latest_network_received_units) }}</strong>
          <small :data-alert="parseInteger(catalog.overview.latest_network_loss_units) > 0n">
            {{ t('citySpatial.services.network.metrics.lossUnits', { value: formatInteger(catalog.overview.latest_network_loss_units) }) }}
          </small>
        </div>
        <div>
          <span>{{ t('citySpatial.services.network.metrics.deliveryRatio') }}</span>
          <strong>{{ formatMilli(catalog.overview.latest_delivery_ratio_milli) }}</strong>
          <small>{{ catalog.profile.policy_version }} · r{{ catalog.profile.revision }}</small>
        </div>
      </div>

      <header class="city-network-toolbar">
        <label>
          <span>{{ t('citySpatial.services.filters.service') }}</span>
          <Select v-model="filters.service" :options="serviceOptions" :searchable="false" />
        </label>
        <label>
          <span>{{ t('citySpatial.services.network.filters.network') }}</span>
          <Select v-model="filters.network" :options="networkOptions" />
        </label>
        <label v-if="activeView === 'topology'">
          <span>{{ t('citySpatial.services.network.filters.edgeStatus') }}</span>
          <Select v-model="filters.status" :options="edgeStatusFilterOptions" :searchable="false" />
        </label>
        <label v-if="activeView === 'facts'">
          <span>{{ t('citySpatial.services.network.filters.phase') }}</span>
          <Select v-model="filters.phase" :options="phaseOptions" :searchable="false" />
        </label>
        <button type="button" class="btn btn-secondary btn-sm" :disabled="loading" @click="applyFilters">
          <Icon name="filter" size="sm" />
          {{ t('citySpatial.services.filters.apply') }}
        </button>
        <div v-if="owner" class="city-network-toolbar-actions">
          <button type="button" class="btn btn-primary btn-sm" :disabled="Boolean(busyCommandCode)" @click="openNetwork()">
            {{ t('citySpatial.services.network.actions.network') }}
          </button>
          <button type="button" class="btn btn-secondary btn-sm" :disabled="Boolean(busyCommandCode) || !selectedNetwork" @click="openNode()">
            {{ t('citySpatial.services.network.actions.node') }}
          </button>
          <button type="button" class="btn btn-secondary btn-sm" :disabled="Boolean(busyCommandCode) || visibleNodes.length < 2" @click="openEdge()">
            {{ t('citySpatial.services.network.actions.edge') }}
          </button>
        </div>
      </header>

      <nav class="city-network-tabs" role="tablist" :aria-label="t('citySpatial.services.network.title')">
        <button
          v-for="view in views"
          :key="view.value"
          type="button"
          role="tab"
          :aria-selected="activeView === view.value"
          :class="{ active: activeView === view.value }"
          @click="activeView = view.value"
        >
          <span>{{ view.index }}</span>
          {{ view.label }}
          <b>{{ view.count }}</b>
        </button>
      </nav>

      <section v-if="activeView === 'topology'" class="city-network-topology" role="tabpanel">
        <div class="city-network-canvas">
          <header>
            <div>
              <strong>{{ selectedNetwork?.name ?? t('citySpatial.services.network.filters.allNetworks') }}</strong>
              <code v-if="selectedNetwork">{{ selectedNetwork.code }} · {{ serviceName(selectedNetwork.service_code) }}</code>
            </div>
            <div v-if="selectedNetwork" class="city-network-canvas-meta">
              <span class="city-network-status" :data-status="selectedNetwork.status">{{ statusName(selectedNetwork.status) }}</span>
              <code>topology r{{ selectedNetwork.topology_revision }} · v{{ selectedNetwork.version }}</code>
            </div>
          </header>

          <div v-if="visibleNodes.length" class="city-network-svg-wrap">
            <svg viewBox="0 0 900 420" role="img" :aria-label="t('citySpatial.services.network.topology.graphLabel')">
              <defs>
                <marker id="city-network-arrow" markerWidth="8" markerHeight="8" refX="7" refY="4" orient="auto" markerUnits="strokeWidth">
                  <path d="M0,0 L8,4 L0,8 Z" />
                </marker>
                <marker id="city-network-arrow-reverse" markerWidth="8" markerHeight="8" refX="1" refY="4" orient="auto" markerUnits="strokeWidth">
                  <path d="M8,0 L0,4 L8,8 Z" />
                </marker>
              </defs>
              <g v-for="edge in graphEdges" :key="edge.code" class="city-network-edge" :data-status="edge.status" :data-selected="selectedEdgeCode === edge.code">
                <line
                  class="city-network-edge-hit"
                  :x1="graphPosition(edge.from_node_code).x"
                  :y1="graphPosition(edge.from_node_code).y"
                  :x2="graphPosition(edge.to_node_code).x"
                  :y2="graphPosition(edge.to_node_code).y"
                  @click="selectEdge(edge.code)"
                />
                <line
                  class="city-network-edge-line"
                  :x1="graphPosition(edge.from_node_code).x"
                  :y1="graphPosition(edge.from_node_code).y"
                  :x2="graphPosition(edge.to_node_code).x"
                  :y2="graphPosition(edge.to_node_code).y"
                  marker-end="url(#city-network-arrow)"
                  :marker-start="edge.direction === 'bidirectional' ? 'url(#city-network-arrow-reverse)' : undefined"
                />
                <text :x="edgeMidpoint(edge).x" :y="edgeMidpoint(edge).y - 8">{{ formatInteger(edge.available_capacity_units) }}</text>
              </g>
              <g
                v-for="node in graphNodes"
                :key="node.item.code"
                class="city-network-node"
                :data-role="node.item.role"
                :data-status="node.item.status"
                :data-selected="selectedNodeCode === node.item.code"
                role="button"
                tabindex="0"
                @click="selectNode(node.item.code)"
                @keydown.enter.prevent="selectNode(node.item.code)"
              >
                <rect :x="node.x - 58" :y="node.y - 25" width="116" height="50" />
                <text class="city-network-node-role" :x="node.x" :y="node.y - 5">{{ roleName(node.item.role) }}</text>
                <text class="city-network-node-code" :x="node.x" :y="node.y + 12">{{ compactCode(node.item.code, 16) }}</text>
              </g>
            </svg>
          </div>
          <div v-else class="city-network-empty city-network-empty-compact">
            <span aria-hidden="true">·</span>
            <p>{{ t('citySpatial.services.network.empty.nodes') }}</p>
          </div>
        </div>

        <aside class="city-network-inspector">
          <template v-if="selectedNode">
            <header>
              <div><span>{{ t('citySpatial.services.network.topology.selectedNode') }}</span><strong>{{ selectedNode.code }}</strong></div>
              <button v-if="owner" type="button" class="btn btn-secondary btn-sm" :disabled="Boolean(busyCommandCode)" @click="openNode(selectedNode)">{{ t('common.edit') }}</button>
            </header>
            <dl>
              <div><dt>{{ t('citySpatial.services.network.columns.role') }}</dt><dd>{{ roleName(selectedNode.role) }}</dd></div>
              <div><dt>{{ t('citySpatial.services.network.columns.status') }}</dt><dd>{{ statusName(selectedNode.status) }}</dd></div>
              <div><dt>{{ t('citySpatial.services.network.columns.binding') }}</dt><dd>{{ nodeBinding(selectedNode) }}</dd></div>
              <div><dt>XYZ</dt><dd>{{ nodeCoordinate(selectedNode) }}</dd></div>
              <div><dt>{{ t('citySpatial.services.network.columns.version') }}</dt><dd>v{{ selectedNode.version }}</dd></div>
            </dl>
          </template>
          <template v-else-if="selectedEdge">
            <header>
              <div><span>{{ t('citySpatial.services.network.topology.selectedEdge') }}</span><strong>{{ selectedEdge.code }}</strong></div>
              <div v-if="owner" class="city-network-inline-actions">
                <button type="button" class="btn btn-secondary btn-sm" :disabled="Boolean(busyCommandCode)" @click="openEdge(selectedEdge)">{{ t('common.edit') }}</button>
                <button v-if="edgeTransitionTargets.length" type="button" class="btn btn-secondary btn-sm" :disabled="Boolean(busyCommandCode)" @click="openEdgeStatus(selectedEdge)">{{ t('citySpatial.services.network.actions.transition') }}</button>
              </div>
            </header>
            <dl>
              <div><dt>{{ t('citySpatial.services.network.columns.route') }}</dt><dd>{{ selectedEdge.from_node_code }} → {{ selectedEdge.to_node_code }}</dd></div>
              <div><dt>{{ t('citySpatial.services.network.columns.status') }}</dt><dd>{{ statusName(selectedEdge.status) }}</dd></div>
              <div><dt>{{ t('citySpatial.services.network.columns.available') }}</dt><dd>{{ formatInteger(selectedEdge.available_capacity_units) }} / {{ formatInteger(selectedEdge.installed_capacity_units) }}</dd></div>
              <div><dt>{{ t('citySpatial.services.network.columns.loss') }}</dt><dd>{{ formatMilli(selectedEdge.loss_milli) }}</dd></div>
              <div><dt>{{ t('citySpatial.services.network.columns.condition') }}</dt><dd>{{ formatMilli(selectedEdge.condition_milli) }}</dd></div>
              <div><dt>{{ t('citySpatial.services.network.columns.version') }}</dt><dd>v{{ selectedEdge.version }}</dd></div>
            </dl>
          </template>
          <template v-else>
            <header><div><span>{{ t('citySpatial.services.network.topology.edgeInventory') }}</span><strong>{{ visibleEdges.length }}</strong></div></header>
            <div class="city-network-edge-list">
              <button v-for="edge in visibleEdges" :key="edge.code" type="button" :data-status="edge.status" @click="selectEdge(edge.code)">
                <span><strong>{{ edge.code }}</strong><small>{{ edge.from_node_code }} → {{ edge.to_node_code }}</small></span>
                <span><b>{{ formatInteger(edge.available_capacity_units) }}</b><small>{{ formatMilli(edge.loss_milli) }}</small></span>
              </button>
            </div>
          </template>
        </aside>
      </section>

      <section v-else-if="activeView === 'flows'" class="city-network-flow-list" role="tabpanel">
        <article v-for="item in visibleFlows" :key="`${item.batch.tick}:${item.batch.sequence}`">
          <header>
            <div>
              <code>T{{ item.batch.tick }}.{{ item.batch.sequence }}</code>
              <strong>{{ item.batch.network_code }} · {{ serviceName(item.batch.service_code) }}</strong>
            </div>
            <span>{{ formatMilli(flowRatio(item)) }}</span>
          </header>
          <div class="city-network-flow-meter"><span :style="{ width: `${flowRatio(item) / 10}%` }" /></div>
          <dl>
            <div><dt>{{ t('citySpatial.services.network.metrics.dispatched') }}</dt><dd>{{ formatInteger(item.batch.dispatched_units) }}</dd></div>
            <div><dt>{{ t('citySpatial.services.network.metrics.received') }}</dt><dd>{{ formatInteger(item.batch.network_received_units) }}</dd></div>
            <div><dt>{{ t('citySpatial.services.network.columns.loss') }}</dt><dd :data-alert="item.batch.network_loss_units > 0">{{ formatInteger(item.batch.network_loss_units) }}</dd></div>
            <div><dt>{{ t('citySpatial.services.network.columns.paths') }}</dt><dd>{{ item.batch.path_count }} / {{ item.batch.segment_count }}</dd></div>
          </dl>
          <details v-for="path in item.paths" :key="`${path.service_sequence}:${path.allocation_index}:${path.path_index}`">
            <summary>
              <span>#{{ path.allocation_index }}.{{ path.path_index }} · {{ path.connection_code }}</span>
              <strong>{{ path.source_node_code }} → {{ path.sink_node_code }}</strong>
              <small>{{ path.hop_count }} hop · {{ formatInteger(path.network_received_units) }} / {{ formatInteger(path.dispatched_units) }}</small>
            </summary>
            <ol class="city-network-segments">
              <li v-for="segment in segmentsForPath(item, path)" :key="segment.segment_index">
                <code>{{ segment.segment_index.toString().padStart(2, '0') }}</code>
                <span><strong>{{ segment.edge_code }}</strong><small>{{ segment.from_node_code }} → {{ segment.to_node_code }}</small></span>
                <span><b>{{ formatInteger(segment.output_units) }} / {{ formatInteger(segment.input_units) }}</b><small>{{ t('citySpatial.services.network.metrics.lossUnits', { value: formatInteger(segment.loss_units) }) }}</small></span>
              </li>
            </ol>
          </details>
        </article>
        <div v-if="visibleFlows.length === 0" class="city-network-empty city-network-empty-compact">
          <span aria-hidden="true">·</span><p>{{ t('citySpatial.services.network.empty.flows') }}</p>
        </div>
      </section>

      <section v-else-if="activeView === 'diagnostics'" class="city-network-diagnostics" role="tabpanel">
        <div v-if="!selectedNetwork" class="city-network-empty city-network-empty-compact">
          <span aria-hidden="true">·</span><p>{{ t('citySpatial.services.network.diagnostics.selectNetwork') }}</p>
        </div>
        <template v-else>
          <form class="city-network-diagnostic-probe" @submit.prevent="runDiagnostics(true)">
            <div>
              <strong>{{ t('citySpatial.services.network.diagnostics.routeProbe') }}</strong>
              <small>{{ t('citySpatial.services.network.diagnostics.routeProbeHint') }}</small>
            </div>
            <label><span>{{ t('citySpatial.services.network.diagnostics.source') }}</span><Select v-model="diagnosticForm.sourceNodeCode" :options="diagnosticNodeOptions" searchable /></label>
            <label><span>{{ t('citySpatial.services.network.diagnostics.sink') }}</span><Select v-model="diagnosticForm.sinkNodeCode" :options="diagnosticNodeOptions" searchable /></label>
            <label><span>{{ t('citySpatial.services.network.diagnostics.probeUnits') }}</span><input v-model.number="diagnosticForm.probeUnits" class="input font-mono" type="number" min="1" max="922337203685477" required /></label>
            <button
              type="submit"
              class="btn btn-primary btn-sm"
              :disabled="loading || !diagnosticForm.sourceNodeCode || !diagnosticForm.sinkNodeCode || diagnosticForm.sourceNodeCode === diagnosticForm.sinkNodeCode || !between(diagnosticForm.probeUnits, 1, 922337203685477)"
            >
              {{ t('citySpatial.services.network.diagnostics.runProbe') }}
            </button>
          </form>

          <template v-if="selectedDiagnostics">
            <div class="city-network-diagnostic-metrics">
              <div><span>{{ t('citySpatial.services.network.diagnostics.components') }}</span><strong>{{ selectedDiagnostics.component_count }}</strong><small>{{ t('citySpatial.services.network.diagnostics.activeAssets', { nodes: selectedDiagnostics.active_node_count, edges: selectedDiagnostics.active_edge_count }) }}</small></div>
              <div :data-alert="selectedDiagnostics.isolated_node_count > 0"><span>{{ t('citySpatial.services.network.diagnostics.isolatedNodes') }}</span><strong>{{ selectedDiagnostics.isolated_node_count }}</strong><small>{{ t('citySpatial.services.network.diagnostics.serviceIslands', { count: selectedDiagnostics.service_island_count }) }}</small></div>
              <div :data-alert="selectedDiagnostics.bottleneck_edge_count > 0"><span>{{ t('citySpatial.services.network.diagnostics.bottlenecks') }}</span><strong>{{ selectedDiagnostics.bottleneck_edge_count }}</strong><small>{{ t('citySpatial.services.network.diagnostics.saturated', { count: selectedDiagnostics.saturated_edge_count }) }}</small></div>
              <div><span>{{ t('citySpatial.services.network.diagnostics.latestFlow') }}</span><strong>{{ selectedDiagnostics.latest_flow_tick == null ? '—' : `T${selectedDiagnostics.latest_flow_tick}` }}</strong><small>{{ selectedDiagnostics.policy?.algorithm_version ?? '—' }}</small></div>
            </div>

            <div class="city-network-diagnostic-grid">
              <section>
                <header><strong>{{ t('citySpatial.services.network.diagnostics.components') }}</strong><span>{{ selectedDiagnostics.components.length }}</span></header>
                <div class="city-network-component-list">
                  <article v-for="component in selectedDiagnostics.components" :key="component.index" :data-alert="component.service_island">
                    <header><code>#{{ component.index.toString().padStart(2, '0') }}</code><strong>{{ t('citySpatial.services.network.diagnostics.componentAssets', { nodes: component.node_count, edges: component.edge_count }) }}</strong></header>
                    <p>{{ component.node_codes.join(' · ') }}</p>
                    <footer><span>S {{ component.supply_node_count }}</span><span>D {{ component.demand_node_count }}</span><b v-if="component.service_island">{{ t('citySpatial.services.network.diagnostics.island') }}</b></footer>
                  </article>
                </div>
              </section>

              <section>
                <header><strong>{{ t('citySpatial.services.network.diagnostics.edgeLoad') }}</strong><span>{{ selectedDiagnostics.edge_diagnostics.length }}</span></header>
                <div class="city-network-edge-diagnostic-list">
                  <button v-for="edge in selectedDiagnostics.edge_diagnostics" :key="edge.edge_code" type="button" :data-alert="edge.bottleneck || edge.saturated" @click="inspectDiagnosticEdge(edge.edge_code)">
                    <span><strong>{{ edge.edge_code }}</strong><small>{{ statusName(edge.status) }} · {{ formatInteger(edge.latest_output_units) }} / {{ formatInteger(edge.latest_input_units) }}</small></span>
                    <span><b>{{ formatMilli(edge.utilization_milli) }}</b><small>{{ t('citySpatial.services.network.metrics.lossUnits', { value: formatInteger(edge.latest_loss_units) }) }}</small></span>
                  </button>
                  <p v-if="selectedDiagnostics.truncated_edge_diagnostic_count" class="city-network-diagnostic-truncated">{{ t('citySpatial.services.network.diagnostics.truncated', { count: selectedDiagnostics.truncated_edge_diagnostic_count }) }}</p>
                </div>
              </section>
            </div>

            <section v-if="selectedDiagnostics.route" class="city-network-route-diagnostic" :data-reachable="selectedDiagnostics.route.reachable">
              <header>
                <div><span>{{ t('citySpatial.services.network.diagnostics.routeResult') }}</span><strong>{{ selectedDiagnostics.route.source_node_code }} → {{ selectedDiagnostics.route.sink_node_code }}</strong></div>
                <b>{{ diagnosticReasonName(selectedDiagnostics.route.reason_code) }}</b>
              </header>
              <dl>
                <div><dt>{{ t('citySpatial.services.network.diagnostics.probeUnits') }}</dt><dd>{{ formatInteger(selectedDiagnostics.route.probe_units) }}</dd></div>
                <div><dt>{{ t('citySpatial.services.network.metrics.dispatched') }}</dt><dd>{{ formatInteger(selectedDiagnostics.route.dispatched_units) }}</dd></div>
                <div><dt>{{ t('citySpatial.services.network.metrics.received') }}</dt><dd>{{ formatInteger(selectedDiagnostics.route.network_received_units) }}</dd></div>
                <div><dt>{{ t('citySpatial.services.network.columns.loss') }}</dt><dd>{{ formatInteger(selectedDiagnostics.route.network_loss_units) }}</dd></div>
              </dl>
              <details v-for="path in selectedDiagnostics.route.paths" :key="path.index">
                <summary><span>#{{ path.index }} · {{ path.segments.length }} hop</span><strong>{{ formatInteger(path.network_received_units) }} / {{ formatInteger(path.dispatched_units) }}</strong><code>{{ compactCode(path.path_hash, 18) }}</code></summary>
                <ol class="city-network-segments">
                  <li v-for="segment in path.segments" :key="segment.index">
                    <code>{{ segment.index.toString().padStart(2, '0') }}</code>
                    <span><strong>{{ segment.edge_code }}</strong><small>{{ segment.from_node_code }} → {{ segment.to_node_code }}</small></span>
                    <span><b>{{ formatInteger(segment.output_units) }} / {{ formatInteger(segment.input_units) }}</b><small>{{ t('citySpatial.services.network.metrics.lossUnits', { value: formatInteger(segment.loss_units) }) }}</small></span>
                  </li>
                </ol>
              </details>
            </section>
          </template>

          <div v-else class="city-network-empty city-network-empty-compact">
            <span aria-hidden="true">⌁</span><p>{{ t('citySpatial.services.network.diagnostics.loading') }}</p>
          </div>
        </template>
      </section>

      <section v-else class="city-network-fact-table-wrap" role="tabpanel">
        <table v-if="visibleFacts.length" class="city-network-fact-table">
          <thead><tr>
            <th>{{ t('citySpatial.services.network.columns.tick') }}</th>
            <th>{{ t('citySpatial.services.network.columns.fact') }}</th>
            <th>{{ t('citySpatial.services.network.columns.subject') }}</th>
            <th>{{ t('citySpatial.services.network.columns.version') }}</th>
            <th>{{ t('citySpatial.services.network.columns.source') }}</th>
          </tr></thead>
          <tbody><tr v-for="fact in visibleFacts" :key="`${fact.tick}:${fact.sequence}`">
            <td><code>T{{ fact.tick }}.{{ fact.sequence }}</code><small>{{ phaseName(fact.phase) }}</small></td>
            <td><strong>{{ factName(fact.fact_type) }}</strong><code>{{ fact.fact_type }}</code></td>
            <td><strong>{{ fact.subject_code }}</strong><small>{{ fact.subject_kind }}</small></td>
            <td><code>v{{ fact.version_before }} → v{{ fact.version_after }}</code></td>
            <td><code>{{ fact.source_command_sequence == null ? t('citySpatial.services.network.facts.automatic') : `C${fact.source_command_sequence}` }}</code></td>
          </tr></tbody>
        </table>
        <div v-else class="city-network-empty city-network-empty-compact">
          <span aria-hidden="true">·</span><p>{{ t('citySpatial.services.network.empty.facts') }}</p>
        </div>
      </section>

      <footer class="city-network-pagination">
        <span>{{ paginationLabel }}</span>
        <div>
          <button v-if="activeView === 'topology' && networks?.next_code" type="button" class="btn btn-secondary btn-sm" :disabled="loading" @click="loadMore('networks')">{{ t('citySpatial.services.network.pagination.networks') }}</button>
          <button v-if="activeView === 'topology' && nodes?.next_code" type="button" class="btn btn-secondary btn-sm" :disabled="loading" @click="loadMore('nodes')">{{ t('citySpatial.services.network.pagination.nodes') }}</button>
          <button v-if="activeView === 'topology' && edges?.next_code" type="button" class="btn btn-secondary btn-sm" :disabled="loading" @click="loadMore('edges')">{{ t('citySpatial.services.network.pagination.edges') }}</button>
          <button v-if="activeView === 'flows' && flows?.next_cursor" type="button" class="btn btn-secondary btn-sm" :disabled="loading" @click="loadMore('flows')">{{ t('citySpatial.services.pagination.more') }}</button>
          <button v-if="activeView === 'facts' && facts?.next_cursor" type="button" class="btn btn-secondary btn-sm" :disabled="loading" @click="loadMore('facts')">{{ t('citySpatial.services.pagination.more') }}</button>
        </div>
      </footer>
    </template>

    <BaseDialog :show="operation !== null" :title="operationTitle" width="wide" @close="closeOperation">
      <form v-if="operation === 'network'" class="city-network-form" @submit.prevent="submitOperation">
        <label><span>{{ t('citySpatial.services.form.code') }}</span><input v-model.trim="networkForm.code" class="input font-mono" maxlength="96" :disabled="networkForm.lockIdentity" required /></label>
        <label><span>{{ t('citySpatial.services.form.name') }}</span><input v-model.trim="networkForm.name" class="input" maxlength="96" required /></label>
        <label><span>{{ t('citySpatial.services.form.service') }}</span><Select v-model="networkForm.serviceCode" :options="policyServiceOptions" :searchable="false" :disabled="networkForm.lockIdentity" /></label>
        <label><span>{{ t('citySpatial.services.form.status') }}</span><Select v-model="networkForm.status" :options="networkStatusOptions" :searchable="false" /></label>
        <div class="city-network-form-preview"><span>{{ t('citySpatial.services.network.form.topologyRevision') }}</span><strong>r{{ selectedNetwork?.topology_revision ?? 0 }}</strong><code>v{{ networkForm.expectedVersion }} → v{{ networkForm.expectedVersion + 1 }}</code></div>
        <p class="city-network-form-note">{{ t('citySpatial.services.network.form.networkNote') }}</p>
      </form>

      <form v-else-if="operation === 'node'" class="city-network-form" @submit.prevent="submitOperation">
        <label><span>{{ t('citySpatial.services.form.code') }}</span><input v-model.trim="nodeForm.code" class="input font-mono" maxlength="96" :disabled="nodeForm.lockIdentity" required /></label>
        <label><span>{{ t('citySpatial.services.network.filters.network') }}</span><Select v-model="nodeForm.networkCode" :options="networkIdentityOptions" :disabled="nodeForm.lockIdentity" /></label>
        <label><span>{{ t('citySpatial.services.network.columns.role') }}</span><Select v-model="nodeForm.role" :options="nodeRoleOptions" :searchable="false" /></label>
        <label><span>{{ t('citySpatial.services.form.status') }}</span><Select v-model="nodeForm.status" :options="nodeStatusOptions" :searchable="false" /></label>
        <label v-if="nodeForm.role === 'supply'"><span>{{ t('citySpatial.services.network.form.capacityBinding') }}</span><Select v-model="nodeForm.capacityCode" :options="capacityOptions" searchable /></label>
        <label v-if="nodeForm.role === 'demand'"><span>{{ t('citySpatial.services.network.form.demandBinding') }}</span><Select v-model="nodeForm.demandCode" :options="demandOptions" searchable /></label>
        <label><span>{{ t('citySpatial.services.filters.district') }}</span><input v-model.trim="nodeForm.districtCode" class="input font-mono" maxlength="96" /></label>
        <label><span>{{ t('citySpatial.services.form.building') }}</span><input v-model.trim="nodeForm.buildingCode" class="input font-mono" maxlength="96" /></label>
        <label class="city-network-coordinate-toggle"><input v-model="nodeForm.hasCoordinates" type="checkbox" /><span>{{ t('citySpatial.services.network.form.anchorCoordinates') }}</span></label>
        <div v-if="nodeForm.hasCoordinates" class="city-network-coordinate-grid">
          <label><span>X</span><input v-model.number="nodeForm.worldX" class="input font-mono" type="number" required /></label>
          <label><span>Y</span><input v-model.number="nodeForm.worldY" class="input font-mono" type="number" required /></label>
          <label><span>Z</span><input v-model.number="nodeForm.worldZ" class="input font-mono" type="number" required /></label>
        </div>
        <p class="city-network-form-note">{{ t('citySpatial.services.network.form.nodeNote') }}</p>
      </form>

      <form v-else-if="operation === 'edge'" class="city-network-form" @submit.prevent="submitOperation">
        <label><span>{{ t('citySpatial.services.form.code') }}</span><input v-model.trim="edgeForm.code" class="input font-mono" maxlength="96" :disabled="edgeForm.lockIdentity" required /></label>
        <label><span>{{ t('citySpatial.services.network.filters.network') }}</span><Select v-model="edgeForm.networkCode" :options="networkIdentityOptions" :disabled="edgeForm.lockIdentity" /></label>
        <label><span>{{ t('citySpatial.services.network.form.fromNode') }}</span><Select v-model="edgeForm.fromNodeCode" :options="edgeNodeOptions" searchable :disabled="edgeForm.lockIdentity" /></label>
        <label><span>{{ t('citySpatial.services.network.form.toNode') }}</span><Select v-model="edgeForm.toNodeCode" :options="edgeNodeOptions" searchable :disabled="edgeForm.lockIdentity" /></label>
        <label><span>{{ t('citySpatial.services.network.form.direction') }}</span><Select v-model="edgeForm.direction" :options="edgeDirectionOptions" :searchable="false" /></label>
        <label><span>{{ t('citySpatial.services.form.status') }}</span><Select v-model="edgeForm.status" :options="edgeStatusOptions" :searchable="false" /></label>
        <label><span>{{ t('citySpatial.services.network.form.installedCapacity') }}</span><input v-model.number="edgeForm.installedCapacityUnits" class="input font-mono" type="number" min="1" max="922337203685477" required /></label>
        <label><span>{{ t('citySpatial.services.form.availability') }}</span><input v-model.number="edgeForm.availabilityMilli" class="input font-mono" type="number" min="0" max="1000" required /></label>
        <label><span>{{ t('citySpatial.services.form.loss') }}</span><input v-model.number="edgeForm.lossMilli" class="input font-mono" type="number" min="0" max="999" required /></label>
        <label><span>{{ t('citySpatial.services.network.form.baseCost') }}</span><input v-model.number="edgeForm.baseCostUnits" class="input font-mono" type="number" min="1" max="922337203685477" required /></label>
        <div class="city-network-form-preview"><span>{{ t('citySpatial.services.network.columns.available') }}</span><strong>{{ formatInteger(edgeCapacityPreview) }}</strong><code>v{{ edgeForm.expectedVersion }} → v{{ edgeForm.expectedVersion + 1 }}</code></div>
        <p class="city-network-form-note">{{ t('citySpatial.services.network.form.edgeNote') }}</p>
      </form>

      <form v-else-if="operation === 'edgeStatus'" class="city-network-form" @submit.prevent="submitOperation">
        <label><span>{{ t('citySpatial.services.network.topology.selectedEdge') }}</span><Select v-model="edgeStatusForm.edgeCode" :options="edgeIdentityOptions" searchable @change="syncEdgeStatus" /></label>
        <label><span>{{ t('citySpatial.services.form.targetStatus') }}</span><Select v-model="edgeStatusForm.toStatus" :options="edgeTransitionOptions" :searchable="false" /></label>
        <div class="city-network-form-preview"><span>{{ t('citySpatial.services.form.currentStatus') }}</span><strong>{{ selectedTransitionEdge ? statusName(selectedTransitionEdge.status) : '—' }}</strong><code>v{{ edgeStatusForm.expectedVersion }} → v{{ edgeStatusForm.expectedVersion + 1 }}</code></div>
        <p class="city-network-form-note">{{ t('citySpatial.services.network.form.transitionNote') }}</p>
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
  CityPhysicalNetwork,
  CityPhysicalNetworkCatalogView,
  CityPhysicalNetworkDiagnosticQuery,
  CityPhysicalNetworkDiagnosticsView,
  CityPhysicalNetworkEdge,
  CityPhysicalNetworkEdgeDirection,
  CityPhysicalNetworkEdgePage,
  CityPhysicalNetworkEdgeStatus,
  CityPhysicalNetworkFactPage,
  CityPhysicalNetworkFlowPage,
  CityPhysicalNetworkFlowPath,
  CityPhysicalNetworkFlowView,
  CityPhysicalNetworkListQuery,
  CityPhysicalNetworkNode,
  CityPhysicalNetworkNodePage,
  CityPhysicalNetworkNodeRole,
  CityPhysicalNetworkNodeStatus,
  CityPhysicalNetworkPage,
  CityPhysicalNetworkStatus,
  CityServiceCatalogView,
  CityServiceCommandType,
  CityServiceDemandPage,
  CityServiceFacilityPage
} from '@/api/citySpatial'
import BaseDialog from '@/components/common/BaseDialog.vue'
import Select, { type SelectOption } from '@/components/common/Select.vue'
import Icon from '@/components/icons/Icon.vue'

type NetworkAvailability = 'unknown' | 'available' | 'unsupported'
type NetworkView = 'topology' | 'flows' | 'diagnostics' | 'facts'
type NetworkSection = 'networks' | 'nodes' | 'edges' | 'flows' | 'facts'
type NetworkOperation = 'network' | 'node' | 'edge' | 'edgeStatus'
interface GraphNode { item: CityPhysicalNetworkNode; x: number; y: number }
interface GraphPoint { x: number; y: number }
interface NodeForm {
  code: string; networkCode: string; role: CityPhysicalNetworkNodeRole
  capacityCode: string; demandCode: string; districtCode: string; buildingCode: string
  hasCoordinates: boolean; worldX: number; worldY: number; worldZ: number
  status: CityPhysicalNetworkNodeStatus; expectedVersion: number; lockIdentity: boolean
}

const props = defineProps<{
  catalog: CityPhysicalNetworkCatalogView | null
  networks: CityPhysicalNetworkPage | null
  nodes: CityPhysicalNetworkNodePage | null
  edges: CityPhysicalNetworkEdgePage | null
  flows: CityPhysicalNetworkFlowPage | null
  facts: CityPhysicalNetworkFactPage | null
  diagnostics: CityPhysicalNetworkDiagnosticsView | null
  serviceCatalog: CityServiceCatalogView
  facilities: CityServiceFacilityPage | null
  demands: CityServiceDemandPage | null
  availability: NetworkAvailability
  owner: boolean
  loading: boolean
  busyCommandCode: string | null
}>()

const emit = defineEmits<{
  (event: 'query', value: { section: NetworkSection; query: CityPhysicalNetworkListQuery; append: boolean }): void
  (event: 'diagnose', value: CityPhysicalNetworkDiagnosticQuery): void
  (event: 'command', value: { commandType: CityServiceCommandType; payload: Record<string, unknown>; commandCode: string }): void
}>()

const { t, te, locale } = useI18n()
const activeView = ref<NetworkView>('topology')
const operation = ref<NetworkOperation | null>(null)
const filters = reactive({ service: '', network: '', status: '', phase: '' })
const selectedNodeCode = ref('')
const selectedEdgeCode = ref('')
const diagnosticForm = reactive({ sourceNodeCode: '', sinkNodeCode: '', probeUnits: 1 })

const networkForm = reactive({ code: '', name: '', serviceCode: '', status: 'active' as CityPhysicalNetworkStatus, expectedVersion: 0, lockIdentity: false })
const nodeForm = reactive<NodeForm>({ code: '', networkCode: '', role: 'junction', capacityCode: '', demandCode: '', districtCode: '', buildingCode: '', hasCoordinates: false, worldX: 0, worldY: 0, worldZ: 0, status: 'active', expectedVersion: 0, lockIdentity: false })
const edgeForm = reactive({ code: '', networkCode: '', fromNodeCode: '', toNodeCode: '', direction: 'directed' as CityPhysicalNetworkEdgeDirection, installedCapacityUnits: 1, availabilityMilli: 1000, lossMilli: 0, baseCostUnits: 1, status: 'active' as CityPhysicalNetworkEdgeStatus, expectedVersion: 0, lockIdentity: false })
const edgeStatusForm = reactive({ edgeCode: '', toStatus: '' as CityPhysicalNetworkEdgeStatus | '', expectedVersion: 0 })

const views = computed(() => [
  { value: 'topology' as const, index: '01', label: t('citySpatial.services.network.tabs.topology'), count: props.nodes?.items.length ?? 0 },
  { value: 'flows' as const, index: '02', label: t('citySpatial.services.network.tabs.flows'), count: props.flows?.items.length ?? 0 },
  { value: 'diagnostics' as const, index: '03', label: t('citySpatial.services.network.tabs.diagnostics'), count: props.diagnostics?.components.length ?? 0 },
  { value: 'facts' as const, index: '04', label: t('citySpatial.services.network.tabs.facts'), count: props.facts?.items.length ?? 0 }
])
const policyServiceOptions = computed<SelectOption[]>(() => props.catalog?.policies.map(policy => ({ value: policy.service_code, label: serviceName(policy.service_code) })) ?? [])
const serviceOptions = computed<SelectOption[]>(() => [{ value: '', label: t('common.all') }, ...policyServiceOptions.value])
const networkIdentityOptions = computed<SelectOption[]>(() => props.networks?.items.filter(network => !filters.service || network.service_code === filters.service).map(network => ({ value: network.code, label: `${network.name} · ${network.code}` })) ?? [])
const networkOptions = computed<SelectOption[]>(() => [{ value: '', label: t('citySpatial.services.network.filters.allNetworks') }, ...networkIdentityOptions.value])
const edgeStatusFilterOptions = computed<SelectOption[]>(() => [{ value: '', label: t('common.all') }, ...['active', 'isolated', 'failed', 'retired'].map(value => ({ value, label: statusName(value) }))])
const phaseOptions = computed<SelectOption[]>(() => [{ value: '', label: t('common.all') }, ...['command', 'pre_network', 'settlement'].map(value => ({ value, label: phaseName(value) }))])
const networkStatusOptions = computed<SelectOption[]>(() => ['active', 'suspended', 'retired'].map(value => ({ value, label: statusName(value) })))
const nodeRoleOptions = computed<SelectOption[]>(() => ['supply', 'demand', 'junction', 'storage', 'gateway'].map(value => ({ value, label: roleName(value) })))
const nodeStatusOptions = computed<SelectOption[]>(() => ['active', 'offline', 'retired'].map(value => ({ value, label: statusName(value) })))
const edgeStatusOptions = computed<SelectOption[]>(() => ['active', 'isolated', 'failed', 'retired'].map(value => ({ value, label: statusName(value) })))
const edgeDirectionOptions = computed<SelectOption[]>(() => ['directed', 'bidirectional'].map(value => ({ value, label: t(`citySpatial.services.network.direction.${value}`) })))
const capacityOptions = computed<SelectOption[]>(() => props.facilities?.items.flatMap(item => item.capacities.map(capacity => ({
  value: `${item.facility.code}.${capacity.service_code}`,
  label: `${item.facility.name} · ${serviceName(capacity.service_code)}`
}))) ?? [])
const demandOptions = computed<SelectOption[]>(() => props.demands?.items.map(item => ({ value: item.demand.code, label: `${item.demand.code} · ${serviceName(item.demand.service_code)}` })) ?? [])
const selectedNetwork = computed(() => filters.network ? props.networks?.items.find(item => item.code === filters.network) ?? null : null)
const visibleNodes = computed(() => props.nodes?.items.filter(item => !selectedNetwork.value || item.network_code === selectedNetwork.value.code) ?? [])
const visibleEdges = computed(() => props.edges?.items.filter(item => (
  (!selectedNetwork.value || item.network_code === selectedNetwork.value.code) && (!filters.status || item.status === filters.status)
)) ?? [])
const visibleFlows = computed(() => props.flows?.items.filter(item => (
  (!selectedNetwork.value || item.batch.network_code === selectedNetwork.value.code) && (!filters.service || item.batch.service_code === filters.service)
)) ?? [])
const visibleFacts = computed(() => props.facts?.items.filter(item => (
  (!filters.phase || item.phase === filters.phase) && factMatchesTopology(item.subject_kind, item.subject_code)
)) ?? [])
const selectedNode = computed(() => visibleNodes.value.find(item => item.code === selectedNodeCode.value) ?? null)
const selectedEdge = computed(() => visibleEdges.value.find(item => item.code === selectedEdgeCode.value) ?? null)
const selectedTransitionEdge = computed(() => props.edges?.items.find(item => item.code === edgeStatusForm.edgeCode) ?? null)
const selectedDiagnostics = computed(() => (
  props.diagnostics?.network?.code === selectedNetwork.value?.code ? props.diagnostics : null
))
const diagnosticNodeOptions = computed<SelectOption[]>(() => visibleNodes.value
  .filter(item => item.status === 'active')
  .map(item => ({ value: item.code, label: `${item.code} · ${roleName(item.role)}` })))
const edgeIdentityOptions = computed<SelectOption[]>(() => visibleEdges.value.filter(item => item.status !== 'retired').map(item => ({ value: item.code, label: `${item.code} · ${statusName(item.status)}` })))
const edgeNodeOptions = computed<SelectOption[]>(() => props.nodes?.items.filter(item => item.network_code === edgeForm.networkCode && item.status !== 'retired').map(item => ({ value: item.code, label: `${item.code} · ${roleName(item.role)}` })) ?? [])
const edgeTransitionTargets = computed(() => transitionTargets(selectedEdge.value?.status ?? ''))
const edgeTransitionOptions = computed<SelectOption[]>(() => transitionTargets(selectedTransitionEdge.value?.status ?? '').map(value => ({ value, label: statusName(value) })))
const edgeCapacityPreview = computed(() => Math.floor(Math.max(0, edgeForm.installedCapacityUnits) * Math.max(0, edgeForm.availabilityMilli) / 1000))
const latestFlowTick = computed(() => props.catalog?.overview?.latest_flow_tick == null ? t('citySpatial.services.metrics.noTick') : `T${props.catalog.overview.latest_flow_tick}`)
const operationTitle = computed(() => operation.value ? t(`citySpatial.services.network.operations.${operation.value}`) : '')
const paginationLabel = computed(() => {
  if (activeView.value === 'flows') return t('citySpatial.services.pagination.loaded', { count: visibleFlows.value.length })
  if (activeView.value === 'diagnostics') return selectedDiagnostics.value
    ? t('citySpatial.services.network.diagnostics.summary', {
        components: selectedDiagnostics.value.component_count,
        bottlenecks: selectedDiagnostics.value.bottleneck_edge_count
      })
    : t('citySpatial.services.network.diagnostics.selectNetwork')
  if (activeView.value === 'facts') return t('citySpatial.services.pagination.loaded', { count: visibleFacts.value.length })
  return t('citySpatial.services.network.pagination.topology', { networks: props.networks?.items.length ?? 0, nodes: visibleNodes.value.length, edges: visibleEdges.value.length })
})
const canSubmitOperation = computed(() => {
  if (operation.value === 'network') return Boolean(networkForm.code && networkForm.name && networkForm.serviceCode && networkForm.expectedVersion >= 0)
  if (operation.value === 'node') {
    const bindingValid = nodeForm.role === 'supply' ? Boolean(nodeForm.capacityCode) : nodeForm.role === 'demand' ? Boolean(nodeForm.demandCode) : true
    const coordinateValid = !nodeForm.hasCoordinates || [nodeForm.worldX, nodeForm.worldY, nodeForm.worldZ].every(Number.isSafeInteger)
    return Boolean(nodeForm.code && nodeForm.networkCode && bindingValid && coordinateValid && nodeForm.expectedVersion >= 0)
  }
  if (operation.value === 'edge') return Boolean(edgeForm.code && edgeForm.networkCode && edgeForm.fromNodeCode && edgeForm.toNodeCode && edgeForm.fromNodeCode !== edgeForm.toNodeCode && between(edgeForm.installedCapacityUnits, 1, 922337203685477) && between(edgeForm.availabilityMilli, 0, 1000) && between(edgeForm.lossMilli, 0, 999) && between(edgeForm.baseCostUnits, 1, 922337203685477) && edgeForm.expectedVersion >= 0)
  if (operation.value === 'edgeStatus') return Boolean(edgeStatusForm.edgeCode && edgeStatusForm.toStatus && edgeTransitionOptions.value.some(item => item.value === edgeStatusForm.toStatus))
  return false
})

const graphNodes = computed<GraphNode[]>(() => {
  const items = visibleNodes.value
  if (!items.length) return []
  const located = items.filter(item => item.world_x != null && item.world_y != null)
  const minX = located.length ? Math.min(...located.map(item => item.world_x!)) : 0
  const maxX = located.length ? Math.max(...located.map(item => item.world_x!)) : 1
  const minY = located.length ? Math.min(...located.map(item => item.world_y!)) : 0
  const maxY = located.length ? Math.max(...located.map(item => item.world_y!)) : 1
  return items.map((item, index) => {
    if (item.world_x != null && item.world_y != null) {
      return {
        item,
        x: 90 + ((item.world_x - minX) / Math.max(1, maxX - minX)) * 720,
        y: 65 + ((item.world_y - minY) / Math.max(1, maxY - minY)) * 290
      }
    }
    const angle = (Math.PI * 2 * index / Math.max(1, items.length)) - Math.PI / 2
    return { item, x: 450 + Math.cos(angle) * 300, y: 210 + Math.sin(angle) * 145 }
  })
})
const graphPositionMap = computed(() => new Map(graphNodes.value.map(item => [item.item.code, { x: item.x, y: item.y }])))
const graphEdges = computed(() => visibleEdges.value.filter(edge => graphPositionMap.value.has(edge.from_node_code) && graphPositionMap.value.has(edge.to_node_code)))

watch(() => `${filters.service}|${props.networks?.items.map(item => item.code).join('|') ?? ''}`, () => {
  const validCodes = new Set(networkIdentityOptions.value.map(item => String(item.value)))
  if (!validCodes.has(filters.network)) filters.network = String(networkIdentityOptions.value[0]?.value ?? '')
}, { immediate: true })
watch(() => selectedNetwork.value?.code ?? '', () => {
  selectedNodeCode.value = ''
  selectedEdgeCode.value = ''
  const nodes = visibleNodes.value.filter(item => item.status === 'active')
  diagnosticForm.sourceNodeCode = nodes.find(item => item.role === 'supply')?.code ?? nodes[0]?.code ?? ''
  diagnosticForm.sinkNodeCode = nodes.find(item => item.role === 'demand' && item.code !== diagnosticForm.sourceNodeCode)?.code ?? nodes.find(item => item.code !== diagnosticForm.sourceNodeCode)?.code ?? ''
  if (selectedNetwork.value) runDiagnostics(false)
}, { immediate: true })
watch(() => edgeForm.networkCode, () => {
  if (!edgeNodeOptions.value.some(item => item.value === edgeForm.fromNodeCode)) edgeForm.fromNodeCode = String(edgeNodeOptions.value[0]?.value ?? '')
  if (!edgeNodeOptions.value.some(item => item.value === edgeForm.toNodeCode) || edgeForm.toNodeCode === edgeForm.fromNodeCode) edgeForm.toNodeCode = String(edgeNodeOptions.value.find(item => item.value !== edgeForm.fromNodeCode)?.value ?? '')
})

function applyFilters(): void {
  const base = baseQuery()
  if (activeView.value === 'topology') {
    emit('query', { section: 'nodes', query: { ...base, limit: 200 }, append: false })
    emit('query', { section: 'edges', query: { ...base, status: filters.status || undefined, limit: 200 }, append: false })
  } else if (activeView.value === 'flows') {
    emit('query', { section: 'flows', query: { ...base, limit: 100 }, append: false })
  } else if (activeView.value === 'diagnostics') {
    runDiagnostics(false)
  } else {
    emit('query', { section: 'facts', query: { ...base, phase: filters.phase || undefined, limit: 100 }, append: false })
  }
}

function runDiagnostics(includeRoute: boolean): void {
  const network = selectedNetwork.value
  if (!network) return
  const query: CityPhysicalNetworkDiagnosticQuery = { network: network.code }
  if (
    includeRoute && diagnosticForm.sourceNodeCode && diagnosticForm.sinkNodeCode &&
    diagnosticForm.sourceNodeCode !== diagnosticForm.sinkNodeCode &&
    between(diagnosticForm.probeUnits, 1, 922337203685477)
  ) {
    query.source = diagnosticForm.sourceNodeCode
    query.sink = diagnosticForm.sinkNodeCode
    query.probe_units = Math.trunc(diagnosticForm.probeUnits)
  }
  emit('diagnose', query)
}

function inspectDiagnosticEdge(code: string): void {
  activeView.value = 'topology'
  selectEdge(code)
}

function loadMore(section: NetworkSection): void {
  const query = baseQuery()
  if (section === 'networks' && props.networks?.next_code) query.after_code = props.networks.next_code
  else if (section === 'nodes' && props.nodes?.next_code) query.after_code = props.nodes.next_code
  else if (section === 'edges' && props.edges?.next_code) query.after_code = props.edges.next_code
  else if (section === 'flows' && props.flows?.next_cursor) {
    query.after_tick = props.flows.next_cursor.tick
    query.after_sequence = props.flows.next_cursor.sequence
  } else if (section === 'facts' && props.facts?.next_cursor) {
    query.after_tick = props.facts.next_cursor.tick
    query.after_sequence = props.facts.next_cursor.sequence
  } else return
  if (section === 'edges' && filters.status) query.status = filters.status
  if (section === 'facts' && filters.phase) query.phase = filters.phase
  query.limit = section === 'flows' || section === 'facts' ? 100 : 200
  emit('query', { section, query, append: true })
}

function baseQuery(): CityPhysicalNetworkListQuery {
  const query: CityPhysicalNetworkListQuery = {}
  if (filters.service) query.service = filters.service
  if (filters.network) query.network = filters.network
  return query
}

function openNetwork(item?: CityPhysicalNetwork): void {
  const network = item ?? selectedNetwork.value
  networkForm.code = network?.code ?? ''
  networkForm.name = network?.name ?? ''
  networkForm.serviceCode = network?.service_code ?? String(policyServiceOptions.value[0]?.value ?? '')
  networkForm.status = network?.status ?? 'active'
  networkForm.expectedVersion = network?.version ?? 0
  networkForm.lockIdentity = Boolean(network)
  operation.value = 'network'
}

function openNode(item?: CityPhysicalNetworkNode): void {
  const node = item
  nodeForm.code = node?.code ?? ''
  nodeForm.networkCode = node?.network_code ?? selectedNetwork.value?.code ?? ''
  nodeForm.role = node?.role ?? 'junction'
  nodeForm.capacityCode = node?.capacity_code ?? ''
  nodeForm.demandCode = node?.demand_code ?? ''
  nodeForm.districtCode = node?.district_code ?? ''
  nodeForm.buildingCode = node?.building_code ?? ''
  nodeForm.hasCoordinates = node?.world_x != null && node.world_y != null && node.world_z != null
  nodeForm.worldX = node?.world_x ?? 0
  nodeForm.worldY = node?.world_y ?? 0
  nodeForm.worldZ = node?.world_z ?? 0
  nodeForm.status = node?.status ?? 'active'
  nodeForm.expectedVersion = node?.version ?? 0
  nodeForm.lockIdentity = Boolean(node)
  operation.value = 'node'
}

function openEdge(item?: CityPhysicalNetworkEdge): void {
  const edge = item
  edgeForm.code = edge?.code ?? ''
  edgeForm.networkCode = edge?.network_code ?? selectedNetwork.value?.code ?? ''
  edgeForm.fromNodeCode = edge?.from_node_code ?? String(edgeNodeOptions.value[0]?.value ?? '')
  edgeForm.toNodeCode = edge?.to_node_code ?? String(edgeNodeOptions.value.find(option => option.value !== edgeForm.fromNodeCode)?.value ?? '')
  edgeForm.direction = edge?.direction ?? 'directed'
  edgeForm.installedCapacityUnits = edge?.installed_capacity_units ?? 1
  edgeForm.availabilityMilli = edge?.availability_milli ?? 1000
  edgeForm.lossMilli = edge?.loss_milli ?? 0
  edgeForm.baseCostUnits = edge?.base_cost_units ?? 1
  edgeForm.status = edge?.status ?? 'active'
  edgeForm.expectedVersion = edge?.version ?? 0
  edgeForm.lockIdentity = Boolean(edge)
  operation.value = 'edge'
}

function openEdgeStatus(item?: CityPhysicalNetworkEdge): void {
  const edge = item ?? selectedEdge.value ?? visibleEdges.value.find(candidate => transitionTargets(candidate.status).length > 0)
  edgeStatusForm.edgeCode = edge?.code ?? ''
  edgeStatusForm.expectedVersion = edge?.version ?? 0
  edgeStatusForm.toStatus = transitionTargets(edge?.status ?? '')[0] ?? ''
  operation.value = 'edgeStatus'
}

function syncEdgeStatus(): void {
  const edge = selectedTransitionEdge.value
  edgeStatusForm.expectedVersion = edge?.version ?? 0
  edgeStatusForm.toStatus = transitionTargets(edge?.status ?? '')[0] ?? ''
}

function closeOperation(): void { operation.value = null }

function submitOperation(): void {
  if (!operation.value || !canSubmitOperation.value) return
  let commandType: CityServiceCommandType
  let commandCode: string
  let payload: Record<string, unknown>
  if (operation.value === 'network') {
    commandType = 'network.configure'
    commandCode = `network:${networkForm.code}`
    payload = { code: networkForm.code, name: networkForm.name, service_code: networkForm.serviceCode, status: networkForm.status, expected_version: networkForm.expectedVersion, metadata: {} }
  } else if (operation.value === 'node') {
    commandType = 'network.node.configure'
    commandCode = `network-node:${nodeForm.code}`
    payload = { code: nodeForm.code, network_code: nodeForm.networkCode, role: nodeForm.role, status: nodeForm.status, expected_version: nodeForm.expectedVersion, metadata: {} }
    if (nodeForm.role === 'supply') payload.capacity_code = nodeForm.capacityCode
    if (nodeForm.role === 'demand') payload.demand_code = nodeForm.demandCode
    if (nodeForm.districtCode) payload.district_code = nodeForm.districtCode
    if (nodeForm.buildingCode) payload.building_code = nodeForm.buildingCode
    if (nodeForm.hasCoordinates) {
      payload.world_x = Math.trunc(nodeForm.worldX)
      payload.world_y = Math.trunc(nodeForm.worldY)
      payload.world_z = Math.trunc(nodeForm.worldZ)
    }
  } else if (operation.value === 'edge') {
    commandType = 'network.edge.configure'
    commandCode = `network-edge:${edgeForm.code}`
    payload = {
      code: edgeForm.code, network_code: edgeForm.networkCode,
      from_node_code: edgeForm.fromNodeCode, to_node_code: edgeForm.toNodeCode,
      direction: edgeForm.direction, installed_capacity_units: Math.trunc(edgeForm.installedCapacityUnits),
      availability_milli: Math.trunc(edgeForm.availabilityMilli), loss_milli: Math.trunc(edgeForm.lossMilli),
      base_cost_units: Math.trunc(edgeForm.baseCostUnits), status: edgeForm.status,
      expected_version: edgeForm.expectedVersion, metadata: {}
    }
  } else {
    commandType = 'network.edge.transition'
    commandCode = `network-edge-status:${edgeStatusForm.edgeCode}`
    payload = { edge_code: edgeStatusForm.edgeCode, to_status: edgeStatusForm.toStatus, expected_version: edgeStatusForm.expectedVersion, metadata: {} }
  }
  emit('command', { commandType, payload, commandCode })
  closeOperation()
}

function selectNode(code: string): void {
  selectedNodeCode.value = selectedNodeCode.value === code ? '' : code
  selectedEdgeCode.value = ''
}

function selectEdge(code: string): void {
  selectedEdgeCode.value = selectedEdgeCode.value === code ? '' : code
  selectedNodeCode.value = ''
}

function graphPosition(code: string): GraphPoint { return graphPositionMap.value.get(code) ?? { x: 0, y: 0 } }
function edgeMidpoint(edge: CityPhysicalNetworkEdge): GraphPoint {
  const from = graphPosition(edge.from_node_code)
  const to = graphPosition(edge.to_node_code)
  return { x: (from.x + to.x) / 2, y: (from.y + to.y) / 2 }
}
function segmentsForPath(item: CityPhysicalNetworkFlowView, path: CityPhysicalNetworkFlowPath) {
  return item.segments.filter(segment => segment.service_sequence === path.service_sequence && segment.allocation_index === path.allocation_index && segment.path_index === path.path_index)
}
function flowRatio(item: CityPhysicalNetworkFlowView): number {
  if (item.batch.dispatched_units <= 0) return 1000
  return Math.max(0, Math.min(1000, Math.floor(item.batch.network_received_units * 1000 / item.batch.dispatched_units)))
}
function nodeBinding(node: CityPhysicalNetworkNode): string { return node.capacity_code ?? node.demand_code ?? node.building_code ?? node.district_code ?? '—' }
function nodeCoordinate(node: CityPhysicalNetworkNode): string { return node.world_x == null || node.world_y == null || node.world_z == null ? '—' : `${node.world_x}, ${node.world_y}, ${node.world_z}` }
function factMatchesTopology(subjectKind: string, subjectCode: string): boolean {
  let networkCode = ''
  if (subjectKind === 'network' || subjectKind === 'flow_batch') networkCode = subjectCode
  else if (subjectKind === 'node') networkCode = props.nodes?.items.find(item => item.code === subjectCode)?.network_code ?? ''
  else if (subjectKind === 'edge') networkCode = props.edges?.items.find(item => item.code === subjectCode)?.network_code ?? ''
  if (filters.network && networkCode !== filters.network) return false
  if (filters.service) return props.networks?.items.some(item => item.code === networkCode && item.service_code === filters.service) ?? false
  return true
}
function compactCode(value: string, maximum: number): string { return value.length <= maximum ? value : `${value.slice(0, maximum - 1)}…` }
function transitionTargets(status: string): CityPhysicalNetworkEdgeStatus[] {
  if (status === 'active') return ['isolated', 'failed', 'retired']
  if (status === 'isolated') return ['active', 'failed', 'retired']
  if (status === 'failed') return ['isolated', 'retired']
  return []
}
function serviceName(code: string): string {
  const key = `citySpatial.services.serviceNames.${code}`
  return te(key) ? t(key) : props.serviceCatalog.service_definitions.find(item => item.code === code)?.name ?? code
}
function roleName(value: string): string {
  const key = `citySpatial.services.network.role.${value}`
  return te(key) ? t(key) : value
}
function statusName(value: string): string {
  const key = `citySpatial.services.network.status.${value}`
  return te(key) ? t(key) : value
}
function phaseName(value: string): string {
  const key = `citySpatial.services.network.phase.${value}`
  return te(key) ? t(key) : value
}
function factName(value: string): string {
  const key = `citySpatial.services.network.factType.${value}`
  return te(key) ? t(key) : value
}
function diagnosticReasonName(value: string): string {
  const key = `citySpatial.services.network.diagnostics.reasons.${value}`
  return te(key) ? t(key) : value
}
function formatInteger(value: string | number | bigint): string { return new Intl.NumberFormat(locale.value, { maximumFractionDigits: 0 }).format(parseInteger(value)) }
function parseInteger(value: string | number | bigint): bigint { try { return BigInt(typeof value === 'number' ? Math.trunc(value) : value) } catch { return 0n } }
function formatMilli(value: number): string { return new Intl.NumberFormat(locale.value, { style: 'percent', maximumFractionDigits: 1 }).format(value / 1000) }
function between(value: number, minimum: number, maximum: number): boolean { return Number.isSafeInteger(value) && value >= minimum && value <= maximum }
</script>

<style scoped>
.city-network-panel { position: relative; min-height: 12rem; }
.city-network-progress { position: absolute; z-index: 4; top: 0; right: 0; left: 0; height: 2px; overflow: hidden; background: color-mix(in srgb, var(--ui-accent) 12%, transparent); }
.city-network-progress span { display: block; width: 32%; height: 100%; background: var(--ui-accent); animation: network-progress 1.1s ease-in-out infinite; }
@keyframes network-progress { from { transform: translateX(-110%); } to { transform: translateX(420%); } }
.city-network-summary { display: grid; grid-template-columns: repeat(6, minmax(8rem, 1fr)); border-bottom: 1px solid var(--ui-separator); }
.city-network-summary > div { min-width: 0; border-right: 1px solid var(--ui-separator); padding: 0.7rem 0.85rem; }
.city-network-summary > div:last-child { border-right: 0; }
.city-network-summary span { display: block; color: var(--ui-label-secondary); font: 0.58rem ui-monospace, monospace; letter-spacing: 0.08em; text-transform: uppercase; }
.city-network-summary strong { display: block; overflow: hidden; margin-top: 0.15rem; font: 1rem ui-monospace, monospace; text-overflow: ellipsis; }
.city-network-summary small { display: block; margin-top: 0.18rem; color: var(--ui-label-secondary); font-size: 0.58rem; }
[data-alert='true'] { color: var(--color-warning, #c7833c) !important; }
.city-network-toolbar { display: grid; grid-template-columns: repeat(3, minmax(10rem, 1fr)) auto minmax(12rem, auto); align-items: end; gap: 0.7rem; border-bottom: 1px solid var(--ui-separator); padding: 0.75rem 1rem; background: var(--ui-control); }
.city-network-toolbar label > span, .city-network-form label > span { display: block; margin-bottom: 0.3rem; color: var(--ui-label-secondary); font-size: 0.64rem; }
.city-network-toolbar-actions, .city-network-inline-actions { display: flex; justify-content: flex-end; gap: 0.35rem; }
.city-network-tabs { display: flex; overflow-x: auto; border-bottom: 1px solid var(--ui-separator); padding: 0 1rem; }
.city-network-tabs button { display: flex; min-height: 2.6rem; flex: none; align-items: center; gap: 0.45rem; border-bottom: 2px solid transparent; padding: 0 0.75rem; color: var(--ui-label-secondary); font-size: 0.68rem; }
.city-network-tabs button.active { border-bottom-color: var(--ui-accent); color: var(--ui-label); background: var(--ui-control); }
.city-network-tabs button > span { color: var(--ui-accent); font: 0.55rem ui-monospace, monospace; }
.city-network-tabs button > b { min-width: 1.2rem; padding: 0.08rem 0.25rem; background: var(--ui-control); font: 0.56rem ui-monospace, monospace; text-align: center; }
.city-network-topology { display: grid; grid-template-columns: minmax(32rem, 2fr) minmax(18rem, 0.8fr); min-height: 30rem; }
.city-network-canvas { min-width: 0; border-right: 1px solid var(--ui-separator); }
.city-network-canvas > header, .city-network-inspector > header { display: flex; min-height: 4rem; align-items: center; justify-content: space-between; gap: 0.75rem; border-bottom: 1px solid var(--ui-separator); padding: 0.65rem 0.85rem; }
.city-network-canvas header strong, .city-network-canvas header code, .city-network-inspector header span, .city-network-inspector header strong { display: block; }
.city-network-canvas header strong { font-size: 0.76rem; }
.city-network-canvas header code { margin-top: 0.18rem; color: var(--ui-label-secondary); font-size: 0.57rem; }
.city-network-canvas-meta { text-align: right; }
.city-network-status { width: fit-content; margin-left: auto; border-left: 3px solid var(--ui-separator); padding: 0.15rem 0.3rem; color: var(--ui-label-secondary); font-size: 0.56rem; }
.city-network-status[data-status='active'] { border-left-color: #16a36a; color: #2a9b6c; }
.city-network-status[data-status='suspended'] { border-left-color: #d99b52; color: #c7833c; }
.city-network-svg-wrap { min-height: 26rem; background: var(--ui-control); }
.city-network-svg-wrap svg { display: block; width: 100%; min-height: 26rem; }
.city-network-svg-wrap marker path { fill: var(--ui-label-secondary); }
.city-network-edge-line { stroke: var(--ui-label-secondary); stroke-width: 2; }
.city-network-edge-hit { cursor: pointer; stroke: transparent; stroke-width: 14; }
.city-network-edge text { fill: var(--ui-label-secondary); font: 11px ui-monospace, monospace; text-anchor: middle; }
.city-network-edge[data-status='isolated'] .city-network-edge-line { stroke: #d99b52; stroke-dasharray: 8 5; }
.city-network-edge[data-status='failed'] .city-network-edge-line { stroke: #d85c5c; stroke-dasharray: 3 5; }
.city-network-edge[data-status='retired'] { opacity: 0.35; }
.city-network-edge[data-selected='true'] .city-network-edge-line { stroke: var(--ui-accent); stroke-width: 4; }
.city-network-node { cursor: pointer; }
.city-network-node rect { fill: var(--ui-surface); stroke: var(--ui-separator); stroke-width: 2; }
.city-network-node:hover rect, .city-network-node[data-selected='true'] rect { stroke: var(--ui-accent); stroke-width: 3; }
.city-network-node[data-status='offline'] { opacity: 0.55; }
.city-network-node-role { fill: var(--ui-label-secondary); font: 10px ui-monospace, monospace; text-anchor: middle; text-transform: uppercase; }
.city-network-node-code { fill: var(--ui-label); font: 11px ui-monospace, monospace; text-anchor: middle; }
.city-network-node[data-role='supply'] rect { fill: color-mix(in srgb, #16a36a 9%, var(--ui-surface)); }
.city-network-node[data-role='demand'] rect { fill: color-mix(in srgb, var(--ui-accent) 9%, var(--ui-surface)); }
.city-network-inspector { min-width: 0; background: var(--ui-control); }
.city-network-inspector header span { color: var(--ui-label-secondary); font-size: 0.56rem; }
.city-network-inspector header strong { margin-top: 0.15rem; overflow-wrap: anywhere; font-size: 0.7rem; }
.city-network-inspector dl { display: grid; grid-template-columns: 1fr 1fr; margin: 0; }
.city-network-inspector dl > div { min-width: 0; border-right: 1px solid var(--ui-separator); border-bottom: 1px solid var(--ui-separator); padding: 0.65rem; }
.city-network-inspector dt { color: var(--ui-label-secondary); font-size: 0.56rem; }
.city-network-inspector dd { overflow-wrap: anywhere; margin: 0.15rem 0 0; font: 0.61rem ui-monospace, monospace; }
.city-network-edge-list { display: grid; gap: 1px; background: var(--ui-separator); }
.city-network-edge-list button { display: grid; grid-template-columns: minmax(8rem, 1fr) auto; gap: 0.6rem; padding: 0.6rem 0.7rem; background: var(--ui-surface); text-align: left; }
.city-network-edge-list button:hover { background: var(--ui-control-hover); }
.city-network-edge-list button > span:last-child { text-align: right; }
.city-network-edge-list strong, .city-network-edge-list small, .city-network-edge-list b { display: block; }
.city-network-edge-list strong, .city-network-edge-list b { font: 0.6rem ui-monospace, monospace; }
.city-network-edge-list small { margin-top: 0.15rem; color: var(--ui-label-secondary); font-size: 0.54rem; }
.city-network-flow-list { display: grid; grid-template-columns: repeat(auto-fit, minmax(25rem, 1fr)); gap: 1px; background: var(--ui-separator); }
.city-network-flow-list > article { min-width: 0; padding: 0.8rem; background: var(--ui-surface); }
.city-network-flow-list > article > header { display: flex; align-items: flex-start; justify-content: space-between; gap: 0.5rem; }
.city-network-flow-list header code, .city-network-flow-list header strong { display: block; }
.city-network-flow-list header code { color: var(--ui-accent); font-size: 0.57rem; }
.city-network-flow-list header strong { margin-top: 0.15rem; font-size: 0.69rem; }
.city-network-flow-list header > span { font: 0.82rem ui-monospace, monospace; }
.city-network-flow-meter { height: 0.28rem; margin-top: 0.6rem; background: var(--ui-control); }
.city-network-flow-meter span { display: block; height: 100%; background: var(--ui-accent); }
.city-network-flow-list dl { display: grid; grid-template-columns: repeat(4, 1fr); margin: 0.65rem 0 0; border: 1px solid var(--ui-separator); }
.city-network-flow-list dl > div { min-width: 0; border-right: 1px solid var(--ui-separator); padding: 0.45rem; }
.city-network-flow-list dl > div:last-child { border-right: 0; }
.city-network-flow-list dt { color: var(--ui-label-secondary); font-size: 0.54rem; }
.city-network-flow-list dd { margin: 0.1rem 0 0; font: 0.61rem ui-monospace, monospace; }
.city-network-flow-list details { margin-top: 0.55rem; border-top: 1px solid var(--ui-separator); padding-top: 0.5rem; }
.city-network-flow-list summary { display: grid; cursor: pointer; grid-template-columns: 1fr 1fr auto; gap: 0.5rem; font-size: 0.58rem; }
.city-network-flow-list summary span, .city-network-flow-list summary small { color: var(--ui-label-secondary); }
.city-network-segments { display: grid; margin: 0.5rem 0 0; padding: 0; gap: 1px; background: var(--ui-separator); list-style: none; }
.city-network-segments li { display: grid; grid-template-columns: auto minmax(8rem, 1fr) auto; align-items: center; gap: 0.55rem; padding: 0.45rem 0.5rem; background: var(--ui-control); }
.city-network-segments strong, .city-network-segments small, .city-network-segments b { display: block; }
.city-network-segments code, .city-network-segments strong, .city-network-segments b { font-size: 0.56rem; }
.city-network-segments small { margin-top: 0.12rem; color: var(--ui-label-secondary); font-size: 0.52rem; }
.city-network-segments li > span:last-child { text-align: right; }
.city-network-diagnostics { background: var(--ui-control); }
.city-network-diagnostic-probe { display: grid; grid-template-columns: minmax(13rem, 1.35fr) repeat(3, minmax(10rem, 1fr)) auto; align-items: end; gap: 0.7rem; border-bottom: 1px solid var(--ui-separator); padding: 0.8rem 1rem; background: var(--ui-surface); }
.city-network-diagnostic-probe > div strong, .city-network-diagnostic-probe > div small { display: block; }
.city-network-diagnostic-probe > div strong { font-size: 0.72rem; }
.city-network-diagnostic-probe > div small { margin-top: 0.2rem; color: var(--ui-label-secondary); font-size: 0.57rem; line-height: 1.45; }
.city-network-diagnostic-probe label > span { display: block; margin-bottom: 0.3rem; color: var(--ui-label-secondary); font-size: 0.6rem; }
.city-network-diagnostic-metrics { display: grid; grid-template-columns: repeat(4, minmax(10rem, 1fr)); gap: 1px; background: var(--ui-separator); }
.city-network-diagnostic-metrics > div { min-width: 0; padding: 0.7rem 0.8rem; background: var(--ui-surface); }
.city-network-diagnostic-metrics span, .city-network-diagnostic-metrics strong, .city-network-diagnostic-metrics small { display: block; }
.city-network-diagnostic-metrics span { color: var(--ui-label-secondary); font-size: 0.56rem; letter-spacing: 0.06em; text-transform: uppercase; }
.city-network-diagnostic-metrics strong { margin-top: 0.16rem; font: 0.9rem ui-monospace, monospace; }
.city-network-diagnostic-metrics small { margin-top: 0.12rem; color: var(--ui-label-secondary); font-size: 0.54rem; }
.city-network-diagnostic-grid { display: grid; grid-template-columns: 1fr 1fr; gap: 1px; background: var(--ui-separator); }
.city-network-diagnostic-grid > section { min-width: 0; background: var(--ui-surface); }
.city-network-diagnostic-grid > section > header { display: flex; min-height: 2.8rem; align-items: center; justify-content: space-between; border-bottom: 1px solid var(--ui-separator); padding: 0.55rem 0.75rem; }
.city-network-diagnostic-grid > section > header strong { font-size: 0.65rem; }
.city-network-diagnostic-grid > section > header span { color: var(--ui-label-secondary); font: 0.58rem ui-monospace, monospace; }
.city-network-component-list, .city-network-edge-diagnostic-list { max-height: 22rem; overflow: auto; }
.city-network-component-list { display: grid; gap: 1px; background: var(--ui-separator); }
.city-network-component-list article { padding: 0.6rem 0.7rem; background: var(--ui-control); }
.city-network-component-list article > header, .city-network-component-list article > footer { display: flex; align-items: center; gap: 0.55rem; }
.city-network-component-list article > header strong { font-size: 0.61rem; }
.city-network-component-list article p { overflow: hidden; margin: 0.4rem 0; color: var(--ui-label-secondary); font: 0.55rem ui-monospace, monospace; text-overflow: ellipsis; white-space: nowrap; }
.city-network-component-list article > footer { color: var(--ui-label-secondary); font: 0.54rem ui-monospace, monospace; }
.city-network-component-list article > footer b { margin-left: auto; color: var(--color-warning, #c7833c); }
.city-network-edge-diagnostic-list button { display: grid; width: 100%; grid-template-columns: minmax(8rem, 1fr) auto; gap: 0.6rem; border-bottom: 1px solid var(--ui-separator); padding: 0.55rem 0.7rem; background: var(--ui-control); text-align: left; }
.city-network-edge-diagnostic-list button:hover { background: var(--ui-control-hover); }
.city-network-edge-diagnostic-list button > span:last-child { text-align: right; }
.city-network-edge-diagnostic-list strong, .city-network-edge-diagnostic-list b, .city-network-edge-diagnostic-list small { display: block; }
.city-network-edge-diagnostic-list strong, .city-network-edge-diagnostic-list b { font: 0.6rem ui-monospace, monospace; }
.city-network-edge-diagnostic-list small { margin-top: 0.12rem; color: var(--ui-label-secondary); font-size: 0.53rem; }
.city-network-diagnostic-truncated { margin: 0; padding: 0.55rem 0.7rem; color: var(--ui-label-secondary); font-size: 0.56rem; }
.city-network-route-diagnostic { border-top: 1px solid var(--ui-separator); padding: 0.8rem; background: var(--ui-surface); }
.city-network-route-diagnostic > header { display: flex; align-items: center; justify-content: space-between; gap: 0.75rem; border-left: 3px solid var(--color-warning, #c7833c); padding-left: 0.65rem; }
.city-network-route-diagnostic[data-reachable='true'] > header { border-left-color: #16a36a; }
.city-network-route-diagnostic > header span, .city-network-route-diagnostic > header strong { display: block; }
.city-network-route-diagnostic > header span { color: var(--ui-label-secondary); font-size: 0.55rem; }
.city-network-route-diagnostic > header strong { margin-top: 0.12rem; font: 0.66rem ui-monospace, monospace; }
.city-network-route-diagnostic > header b { font-size: 0.62rem; }
.city-network-route-diagnostic dl { display: grid; grid-template-columns: repeat(4, 1fr); margin: 0.7rem 0; border: 1px solid var(--ui-separator); }
.city-network-route-diagnostic dl > div { border-right: 1px solid var(--ui-separator); padding: 0.45rem 0.55rem; }
.city-network-route-diagnostic dl > div:last-child { border-right: 0; }
.city-network-route-diagnostic dt { color: var(--ui-label-secondary); font-size: 0.54rem; }
.city-network-route-diagnostic dd { margin: 0.12rem 0 0; font: 0.61rem ui-monospace, monospace; }
.city-network-route-diagnostic details { border-top: 1px solid var(--ui-separator); padding: 0.5rem 0; }
.city-network-route-diagnostic summary { display: grid; cursor: pointer; grid-template-columns: 1fr auto auto; gap: 0.7rem; font-size: 0.57rem; }
.city-network-route-diagnostic summary span, .city-network-route-diagnostic summary code { color: var(--ui-label-secondary); }
.city-network-fact-table-wrap { overflow-x: auto; }
.city-network-fact-table { width: 100%; min-width: 56rem; border-collapse: collapse; }
.city-network-fact-table th { border-bottom: 1px solid var(--ui-separator); padding: 0.65rem 0.75rem; color: var(--ui-label-secondary); background: var(--ui-control); font-size: 0.6rem; text-align: left; }
.city-network-fact-table td { border-bottom: 1px solid var(--ui-separator); padding: 0.65rem 0.75rem; font-size: 0.64rem; vertical-align: top; }
.city-network-fact-table td > strong, .city-network-fact-table td > code, .city-network-fact-table td > small { display: block; }
.city-network-fact-table td > code, .city-network-fact-table td > small { margin-top: 0.12rem; color: var(--ui-label-secondary); font-size: 0.54rem; }
.city-network-pagination { display: flex; min-height: 3.4rem; align-items: center; justify-content: space-between; gap: 0.7rem; border-top: 1px solid var(--ui-separator); padding: 0.6rem 1rem; }
.city-network-pagination > span { color: var(--ui-label-secondary); font-size: 0.6rem; }
.city-network-pagination > div { display: flex; flex-wrap: wrap; gap: 0.35rem; }
.city-network-empty { display: grid; min-height: 11rem; place-content: center; justify-items: center; gap: 0.5rem; color: var(--ui-label-secondary); }
.city-network-empty > span { color: var(--ui-accent); font: 1.5rem ui-monospace, monospace; }
.city-network-empty p { margin: 0; font-size: 0.7rem; }
.city-network-empty-compact { min-height: 18rem; background: var(--ui-surface); }
.city-network-unsupported { display: flex; min-height: 11rem; align-items: center; justify-content: center; gap: 1rem; padding: 1rem; }
.city-network-unsupported > code { border: 1px solid var(--ui-separator); padding: 0.75rem; color: var(--ui-accent); background: var(--ui-control); }
.city-network-unsupported strong { font-size: 0.8rem; }
.city-network-unsupported p { max-width: 38rem; margin: 0.25rem 0 0; color: var(--ui-label-secondary); font-size: 0.68rem; }
.city-network-form { display: grid; grid-template-columns: 1fr 1fr; gap: 0.85rem; }
.city-network-form-note { grid-column: 1 / -1; margin: 0; border-left: 3px solid var(--ui-accent); padding-left: 0.65rem; color: var(--ui-label-secondary); font-size: 0.62rem; }
.city-network-form-preview { display: grid; grid-template-columns: 1fr auto; align-content: center; border: 1px solid var(--ui-separator); padding: 0.55rem 0.65rem; background: var(--ui-control); }
.city-network-form-preview span { color: var(--ui-label-secondary); font-size: 0.58rem; }
.city-network-form-preview strong { grid-row: 2; margin-top: 0.12rem; font: 0.72rem ui-monospace, monospace; }
.city-network-form-preview code { grid-row: 1 / span 2; grid-column: 2; align-self: center; color: var(--ui-label-secondary); font-size: 0.58rem; }
.city-network-coordinate-toggle { display: flex; align-items: center; gap: 0.45rem; }
.city-network-coordinate-toggle > span { margin: 0 !important; }
.city-network-coordinate-grid { display: grid; grid-template-columns: repeat(3, 1fr); gap: 0.5rem; }
@media (max-width: 1100px) { .city-network-summary { grid-template-columns: repeat(3, 1fr); } .city-network-summary > div:nth-child(3) { border-right: 0; } .city-network-toolbar { grid-template-columns: repeat(2, minmax(10rem, 1fr)); } .city-network-topology { grid-template-columns: 1fr; } .city-network-canvas { border-right: 0; } .city-network-diagnostic-probe { grid-template-columns: repeat(2, minmax(10rem, 1fr)); } .city-network-diagnostic-probe > div { grid-column: 1 / -1; } }
@media (max-width: 720px) { .city-network-summary { grid-template-columns: repeat(2, 1fr); } .city-network-summary > div:nth-child(3) { border-right: 1px solid var(--ui-separator); } .city-network-summary > div:nth-child(even) { border-right: 0; } .city-network-toolbar, .city-network-form, .city-network-diagnostic-probe, .city-network-diagnostic-grid { grid-template-columns: 1fr; } .city-network-diagnostic-probe > div { grid-column: auto; } .city-network-diagnostic-metrics { grid-template-columns: repeat(2, 1fr); } .city-network-route-diagnostic dl { grid-template-columns: repeat(2, 1fr); } .city-network-toolbar-actions { justify-content: flex-start; } .city-network-form-note { grid-column: auto; } .city-network-flow-list { grid-template-columns: 1fr; } .city-network-flow-list summary, .city-network-segments li, .city-network-route-diagnostic summary { grid-template-columns: 1fr; } .city-network-segments li > span:last-child { text-align: left; } }
</style>
