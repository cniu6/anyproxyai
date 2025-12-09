<template>
  <n-config-provider :theme="isDark ? darkTheme : null" :theme-overrides="themeOverrides">
    <n-layout style="height: 100vh;">
      <!-- Top Bar -->
      <n-layout-header bordered style="height: 64px; padding: 0 24px; display: flex; align-items: center; justify-content: space-between;">
        <div style="display: flex; align-items: center; gap: 12px;">
          <n-icon size="32" color="#18a058">
            <ServerIcon />
          </n-icon>
          <span style="font-size: 20px; font-weight: 600;">AnyProxyAi</span>
        </div>

        <!-- Navigation Tabs -->
        <div style="display: flex; align-items: center; gap: 8px;">
          <n-button
            :type="currentPage === 'home' ? 'primary' : 'default'"
            :ghost="currentPage !== 'home'"
            @click="currentPage = 'home'"
          >
            <template #icon>
              <n-icon><HomeIcon /></n-icon>
            </template>
            {{ t('nav.home') }}
          </n-button>

          <n-button
            :type="currentPage === 'models' ? 'primary' : 'default'"
            :ghost="currentPage !== 'models'"
            @click="currentPage = 'models'"
          >
            <template #icon>
              <n-icon><ListIcon /></n-icon>
            </template>
            {{ t('nav.models') }}
          </n-button>

          <n-button
            :type="currentPage === 'stats' ? 'primary' : 'default'"
            :ghost="currentPage !== 'stats'"
            @click="currentPage = 'stats'"
          >
            <template #icon>
              <n-icon><BarChartIcon /></n-icon>
            </template>
            {{ t('nav.stats') }}
          </n-button>
        </div>

        <div style="display: flex; align-items: center; gap: 16px;">
          <n-button quaternary circle @click="refreshAll" :loading="refreshing">
            <template #icon>
              <n-icon :size="20">
                <RefreshIcon />
              </n-icon>
            </template>
          </n-button>

          <n-button quaternary circle @click="currentPage = 'settings'">
            <template #icon>
              <n-icon :size="20">
                <SettingsIcon />
              </n-icon>
            </template>
          </n-button>

          <n-button quaternary circle @click="toggleTheme">
            <template #icon>
              <n-icon>
                <MoonIcon v-if="isDark" />
                <SunnyIcon v-else />
              </n-icon>
            </template>
          </n-button>

          <n-button quaternary circle @click="showLanguageModal = true">
            <template #icon>
              <n-icon :size="20">
                <LanguageIcon />
              </n-icon>
            </template>
          </n-button>

          <n-button type="primary" @click="showAddModal = true">
            <template #icon>
              <n-icon><AddIcon /></n-icon>
            </template>
            {{ t('nav.addRoute') }}
          </n-button>
        </div>
      </n-layout-header>

      <!-- Main Content -->
      <n-layout-content style="padding: 24px; overflow: auto;">
        <!-- Home Page -->
        <div v-if="currentPage === 'home'">
          <!-- Stats Cards -->
          <n-grid :cols="4" :x-gap="16" :y-gap="16" style="margin-bottom: 24px;">
            <n-grid-item>
              <n-card :bordered="false" style="background: linear-gradient(135deg, #667eea 0%, #764ba2 100%);">
                <n-statistic :label="t('home.routeCount')" :value="stats.route_count">
                  <template #prefix>
                    <n-icon size="24" color="#fff">
                      <GitNetworkIcon />
                    </n-icon>
                  </template>
                </n-statistic>
              </n-card>
            </n-grid-item>

            <n-grid-item>
              <n-card :bordered="false" style="background: linear-gradient(135deg, #f093fb 0%, #f5576c 100%);">
                <n-statistic :label="t('home.modelCount')" :value="stats.model_count">
                  <template #prefix>
                    <n-icon size="24" color="#fff">
                      <CubeIcon />
                    </n-icon>
                  </template>
                </n-statistic>
              </n-card>
            </n-grid-item>

            <n-grid-item>
              <n-card :bordered="false" style="background: linear-gradient(135deg, #4facfe 0%, #00f2fe 100%);">
                <n-statistic :label="t('home.totalRequests')" :value="stats.total_requests">
                  <template #prefix>
                    <n-icon size="24" color="#fff">
                      <StatsChartIcon />
                    </n-icon>
                  </template>
                </n-statistic>
              </n-card>
            </n-grid-item>

            <n-grid-item>
              <n-card :bordered="false" style="background: linear-gradient(135deg, #43e97b 0%, #38f9d7 100%);">
                <n-statistic :label="t('home.todayTokens')" :value="formatNumber(stats.today_tokens || 0)">
                  <template #prefix>
                    <n-icon size="24" color="#fff">
                      <FlashIcon />
                    </n-icon>
                  </template>
                </n-statistic>
              </n-card>
            </n-grid-item>
          </n-grid>

          <!-- Redirect Config -->
          <n-card :title="'🔀 ' + t('home.redirectConfig')" style="margin-bottom: 24px;" :bordered="false">
            <n-space vertical>
              <n-space align="center">
                <span>{{ t('home.enableRedirect') }}:</span>
                <n-switch v-model:value="redirectConfig.enabled" @update:value="saveRedirectConfig" />
              </n-space>

              <n-space align="center" v-if="redirectConfig.enabled">
                <n-tag type="info" size="large" style="font-family: monospace;">
                  {{ redirectConfig.keyword }}
                </n-tag>
                <n-icon size="20"><ArrowForwardIcon /></n-icon>
                <n-tag type="success" size="large" style="font-family: monospace;">
                  {{ redirectConfig.targetModel || t('home.notConfigured') }}
                </n-tag>
                <n-tag v-if="redirectConfig.targetName" type="warning" size="large">
                  ({{ redirectConfig.targetName }})
                </n-tag>
                <!-- 跳转按钮 -->
                <n-button
                  v-if="redirectConfig.targetModel"
                  size="small"
                  @click="jumpToTargetModel"
                >
                  <template #icon>
                    <n-icon><LocationIcon /></n-icon>
                  </template>
                  {{ t('home.jumpToTarget') }}
                </n-button>
              </n-space>
            </n-space>
          </n-card>

          <!-- API Config -->
          <n-card :title="'🔑 ' + t('home.apiConfig')" style="margin-bottom: 24px;" :bordered="false">
            <n-grid :cols="2" :x-gap="24">
              <!-- 左侧: OpenAI 兼容接口 -->
              <n-grid-item>
                <n-space vertical :size="12">
                  <n-text strong style="font-size: 14px;">{{ t('home.openaiInterface') }}</n-text>
                  <n-text depth="3" style="font-size: 12px;">{{ t('home.openaiInterfaceDesc') }}</n-text>

                  <div>
                    <n-text depth="2" style="font-size: 13px; margin-bottom: 4px; display: block;">{{ t('home.apiAddress') }}</n-text>
                    <n-input
                      :value="config.localApiEndpoint + '/api'"
                      readonly
                      size="large"
                    >
                      <template #suffix>
                        <n-button text @click="copyToClipboard(config.localApiEndpoint + '/api')">
                          <template #icon>
                            <n-icon><CopyIcon /></n-icon>
                          </template>
                        </n-button>
                      </template>
                    </n-input>
                    <n-text depth="3" style="font-size: 11px; margin-top: 4px; display: block; color: #18a058;">
                      📝 {{ t('home.openaiPath') }}：{{ config.localApiEndpoint }}/api/v1/chat/completions
                    </n-text>
                  </div>

                  <div>
                    <n-text depth="2" style="font-size: 13px; margin-bottom: 4px; display: block;">{{ t('home.apiKey') }}</n-text>
                    <n-input
                      :value="maskApiKey(config.localApiKey)"
                      readonly
                      size="large"
                    >
                      <template #suffix>
                        <n-button text @click="copyToClipboard(config.localApiKey)">
                          <template #icon>
                            <n-icon><CopyIcon /></n-icon>
                          </template>
                        </n-button>
                        <n-button text @click="generateNewApiKey" style="margin-left: 8px;">
                          <template #icon>
                            <n-icon><RefreshIcon /></n-icon>
                          </template>
                        </n-button>
                      </template>
                    </n-input>
                  </div>
                </n-space>
              </n-grid-item>

              <!-- 右侧: 翻译 API 接口 -->
              <n-grid-item>
                <n-space vertical :size="12">
                  <n-text strong style="font-size: 14px;">{{ t('home.translationInterface') }}</n-text>
                  <n-text depth="3" style="font-size: 12px;">{{ t('home.translationInterfaceDesc') }}</n-text>

                  <div>
                    <n-text depth="2" style="font-size: 13px; margin-bottom: 4px; display: block;">{{ t('home.claudeCodeInterface') }}</n-text>
                    <n-input
                      :value="config.localApiEndpoint + '/api/claudecode'"
                      readonly
                      size="large"
                    >
                      <template #suffix>
                        <n-button text @click="copyToClipboard(config.localApiEndpoint + '/api/claudecode')">
                          <template #icon>
                            <n-icon><CopyIcon /></n-icon>
                          </template>
                        </n-button>
                      </template>
                    </n-input>
                    <n-text depth="3" style="font-size: 11px; margin-top: 4px; display: block; color: #18a058;">
                      📝 {{ t('home.claudeCodePath') }}：{{ config.localApiEndpoint }}/api/claudecode/v1/messages
                    </n-text>
                  </div>

                  <div>
                    <n-text depth="2" style="font-size: 13px; margin-bottom: 4px; display: block;">{{ t('home.anthropicInterface') }}</n-text>
                    <n-input
                      :value="config.localApiEndpoint + '/api/anthropic'"
                      readonly
                      size="large"
                    >
                      <template #suffix>
                        <n-button text @click="copyToClipboard(config.localApiEndpoint + '/api/anthropic')">
                          <template #icon>
                            <n-icon><CopyIcon /></n-icon>
                          </template>
                        </n-button>
                      </template>
                    </n-input>
                    <n-text depth="3" style="font-size: 11px; margin-top: 4px; display: block; color: #18a058;">
                      📝 {{ t('home.anthropicPath') }}：{{ config.localApiEndpoint }}/api/anthropic/v1/messages
                    </n-text>
                  </div>

                  <div>
                    <n-text depth="2" style="font-size: 13px; margin-bottom: 4px; display: block;">{{ t('home.geminiInterface') }}</n-text>
                    <n-input
                      :value="config.localApiEndpoint + '/api/gemini'"
                      readonly
                      size="large"
                    >
                      <template #suffix>
                        <n-button text @click="copyToClipboard(config.localApiEndpoint + '/api/gemini')">
                          <template #icon>
                            <n-icon><CopyIcon /></n-icon>
                          </template>
                        </n-button>
                      </template>
                    </n-input>
                    <n-text depth="3" style="font-size: 11px; margin-top: 4px; display: block; color: #18a058;">
                      📝 {{ t('home.geminiPath') }}：{{ config.localApiEndpoint }}/api/gemini/completions
                    </n-text>
                  </div>
                </n-space>
              </n-grid-item>
            </n-grid>
          </n-card>
        </div>

        <!-- Models Page -->
        <div v-if="currentPage === 'models'">
          <n-card :title="'📋 ' + t('models.title')" :bordered="false">
            <template #header-extra>
              <n-space>
                <n-button @click="exportRoutes" type="primary" ghost>
                  <template #icon>
                    <n-icon><ArrowForwardIcon style="transform: rotate(-90deg);" /></n-icon>
                  </template>
                  {{ t('models.exportJson') }}
                </n-button>
                <n-button @click="triggerImport" type="primary" ghost>
                  <template #icon>
                    <n-icon><ArrowForwardIcon style="transform: rotate(90deg);" /></n-icon>
                  </template>
                  {{ t('models.importJson') }}
                </n-button>
                <n-button @click="loadRoutes" quaternary circle>
                  <template #icon>
                    <n-icon><RefreshIcon /></n-icon>
                  </template>
                </n-button>
              </n-space>
              <input
                ref="fileInput"
                type="file"
                accept=".json"
                style="display: none;"
                @change="handleFileImport"
              />
            </template>

            <!-- 按分组显示的折叠面板 -->
            <n-collapse v-model:expanded-names="expandedGroups">
              <n-collapse-item
                v-for="(groupRoutes, groupName) in groupedRoutes"
                :key="groupName"
                :name="groupName"
                :title="`${t('models.group')}: ${groupName || t('models.ungrouped')} (${groupRoutes.length} ${t('models.modelCount')})`"
              >
                <n-data-table
                  :columns="modelsPageColumns"
                  :data="groupRoutes"
                  :bordered="false"
                  :single-line="false"
                  size="small"
                  striped
                  :pagination="false"
                  :row-props="rowProps"
                />
              </n-collapse-item>
            </n-collapse>

            <n-empty
              v-if="routes.length === 0"
              :description="t('models.noRoutes')"
              style="margin: 60px 0;"
            />
          </n-card>
        </div>

        <!-- Stats Page -->
        <div v-if="currentPage === 'stats'">
          <n-space vertical :size="16">
            <!-- 今日消耗统计卡片 -->
            <n-card :title="'📊 ' + t('stats.todayStats')" :bordered="false">
              <template #header-extra>
                <n-button type="error" quaternary @click="showClearStatsDialog">
                  <template #icon>
                    <n-icon><TrashIcon /></n-icon>
                  </template>
                  {{ t('stats.clearData') }}
                </n-button>
              </template>
              <n-grid :cols="4" :x-gap="16">
                <n-grid-item>
                  <n-statistic :label="t('stats.todayTokens')" :value="formatNumber(stats.today_tokens || 0)">
                    <template #prefix>
                      <n-icon size="20" color="#18a058">
                        <FlashIcon />
                      </n-icon>
                    </template>
                  </n-statistic>
                </n-grid-item>
                <n-grid-item>
                  <n-statistic :label="t('stats.todayRequests')" :value="stats.today_requests || 0">
                    <template #prefix>
                      <n-icon size="20" color="#18a058">
                        <StatsChartIcon />
                      </n-icon>
                    </template>
                  </n-statistic>
                </n-grid-item>
                <n-grid-item>
                  <n-statistic :label="t('stats.totalTokens')" :value="formatNumber(stats.total_tokens)">
                    <template #prefix>
                      <n-icon size="20" color="#18a058">
                        <FlashIcon />
                      </n-icon>
                    </template>
                  </n-statistic>
                </n-grid-item>
                <n-grid-item>
                  <n-statistic :label="t('stats.totalRequests')" :value="stats.total_requests">
                    <template #prefix>
                      <n-icon size="20" color="#18a058">
                        <StatsChartIcon />
                      </n-icon>
                    </template>
                  </n-statistic>
                </n-grid-item>
              </n-grid>
            </n-card>

            <!-- GitHub 热力图样式的历史使用量 -->
            <n-card :title="'🔥 ' + t('stats.heatmap')" :bordered="false">
              <div class="heatmap-container" @mouseleave="heatmapTooltip.show = false">
                <div class="heatmap-months-row">
                  <span 
                    v-for="monthData in heatmapMonthsWithPosition" 
                    :key="monthData.weekIndex"
                    class="heatmap-month-label"
                    :style="{ left: (monthData.weekIndex / 53 * 100) + '%' }"
                  >{{ monthData.name }}</span>
                </div>
                <div class="heatmap-grid">
                  <div v-for="(week, weekIndex) in heatmapData" :key="weekIndex" class="heatmap-week">
                    <div
                      v-for="(day, dayIndex) in week"
                      :key="dayIndex"
                      class="heatmap-cell"
                      :class="getHeatmapClass(day.tokens)"
                      @mouseenter="showHeatmapTooltip($event, day)"
                      @mouseleave="heatmapTooltip.show = false"
                    ></div>
                  </div>
                </div>
                <!-- 单一 tooltip 元素 -->
                <div 
                  v-show="heatmapTooltip.show" 
                  class="heatmap-tooltip"
                  :style="{ left: heatmapTooltip.x + 'px', top: heatmapTooltip.y + 'px' }"
                >
                  <div style="font-weight: bold;">{{ t('stats.date') }}: {{ heatmapTooltip.date }}</div>
                  <div>{{ t('stats.tokens') }}: {{ formatNumber(heatmapTooltip.tokens) }}</div>
                  <div>{{ t('stats.requestCount') }}: {{ heatmapTooltip.requests }}</div>
                </div>
                <div class="heatmap-legend">
                  <span>{{ t('stats.less') }}</span>
                  <div class="legend-box level-0"></div>
                  <div class="legend-box level-1"></div>
                  <div class="legend-box level-2"></div>
                  <div class="legend-box level-3"></div>
                  <div class="legend-box level-4"></div>
                  <span>{{ t('stats.more') }}</span>
                </div>
              </div>
            </n-card>

            <!-- 今日按时间段显示的折线图 -->
            <n-card :title="'📈 ' + t('stats.todayTrend')" :bordered="false">
              <v-chart :option="todayChartOption" style="height: 300px;" :theme="isDark ? 'dark' : ''" autoresize />
            </n-card>

            <!-- 历史使用量 - 接口使用排行 -->
            <n-card :title="'🏆 ' + t('stats.modelRanking')" :bordered="false">
              <n-data-table
                :columns="rankingColumns"
                :data="modelRankingData"
                :pagination="false"
                :bordered="false"
                striped
              />
            </n-card>
          </n-space>
        </div>

        <!-- Settings Page -->
        <div v-if="currentPage === 'settings'">
          <n-card :title="'⚙️ ' + t('settings.title')" :bordered="false">
            <n-space vertical :size="24">
              <!-- GitHub 项目信息 -->
              <div>
                <n-text strong style="font-size: 16px;">{{ t('settings.projectInfo') }}</n-text>
                <n-space vertical :size="12" style="margin-top: 12px;">
                  <n-space align="center">
                    <n-icon size="20"><LogoGithubIcon /></n-icon>
                    <n-text>{{ t('settings.githubRepo') }}:</n-text>
                    <n-button text type="primary" tag="a" href="https://github.com/cniu6/anyproxyai" target="_blank">
                      github.com/cniu6/anyproxyai
                    </n-button>
                  </n-space>

                  <n-space align="center">
                    <n-icon size="20"><InformationCircleIcon /></n-icon>
                    <n-text>{{ t('settings.version') }}: v2.0.2</n-text>
                  </n-space>

                  <n-space align="center">
                    <n-icon size="20"><CodeIcon /></n-icon>
                    <n-text>{{ t('settings.builtWith') }}</n-text>
                  </n-space>
                </n-space>
              </div>

              <n-divider />

              <!-- 应用选项 -->
              <div>
                <n-text strong style="font-size: 16px;">{{ t('settings.appOptions') }}</n-text>
                <n-space vertical :size="16" style="margin-top: 12px;">
                  <!-- 重定向关键字设置 -->
                  <div>
                    <n-text depth="2" style="font-size: 14px; margin-bottom: 8px; display: block;">{{ t('settings.redirectKeyword') }}</n-text>
                    <n-input
                      v-model:value="settings.redirectKeyword"
                      placeholder="proxy_auto"
                      style="max-width: 300px;"
                    >
                      <template #suffix>
                        <n-button text size="small" @click="updateRedirectKeyword">
                          {{ t('settings.save') }}
                        </n-button>
                      </template>
                    </n-input>
                    <n-text depth="3" style="font-size: 12px; margin-top: 4px; display: block;">
                      {{ t('settings.redirectKeywordDesc') }}
                    </n-text>
                  </div>

                  <n-checkbox v-model:checked="settings.autoStart" @update:checked="toggleAutoStart">
                    {{ t('settings.autoStart') }}
                  </n-checkbox>

                  <n-checkbox v-model:checked="settings.minimizeToTray" @update:checked="toggleMinimizeToTray">
                    {{ t('settings.minimizeToTray') }}
                  </n-checkbox>

                  <n-checkbox v-model:checked="settings.enableFileLog" @update:checked="toggleEnableFileLog">
                    {{ t('settings.enableFileLog') }}
                  </n-checkbox>
                  <n-text depth="3" style="font-size: 12px; margin-left: 24px;">
                    {{ t('settings.enableFileLogDesc') }}
                  </n-text>
                </n-space>
              </div>

              <n-divider />

              <!-- 语言设置 -->
              <div>
                <n-text strong style="font-size: 16px;">{{ t('settings.languageSettings') }}</n-text>
                <n-space align="center" style="margin-top: 12px;">
                  <n-text>{{ t('settings.language') }}:</n-text>
                  <n-select
                    :value="currentLocale"
                    @update:value="switchLanguage"
                    :options="[
                      { label: '🇨🇳 简体中文', value: 'zh-CN' },
                      { label: '🇺🇸 English', value: 'en-US' }
                    ]"
                    style="width: 160px;"
                  />
                </n-space>
                <n-text depth="3" style="font-size: 12px; margin-top: 4px; display: block;">
                  {{ t('settings.languageDesc') }}
                </n-text>
              </div>

              <n-divider />

              <!-- 主题设置 -->
              <div>
                <n-text strong style="font-size: 16px;">{{ t('settings.themeSettings') }}</n-text>
                <n-space align="center" style="margin-top: 12px;">
                  <n-text>{{ t('settings.currentTheme') }}:</n-text>
                  <n-tag :type="isDark ? 'info' : 'warning'">
                    {{ isDark ? t('settings.darkMode') : t('settings.lightMode') }}
                  </n-tag>
                  <n-button @click="toggleTheme">
                    <template #icon>
                      <n-icon>
                        <MoonIcon v-if="!isDark" />
                        <SunnyIcon v-else />
                      </n-icon>
                    </template>
                    {{ t('settings.switchTheme') }}
                  </n-button>
                </n-space>
              </div>
            </n-space>
          </n-card>
        </div>
      </n-layout-content>
    </n-layout>

    <!-- Add Route Modal -->
    <AddRouteModal 
      v-model:visible="showAddModal" 
      @route-added="handleRouteAdded" 
    />
    
    <!-- Edit Route Modal -->
    <EditRouteModal
      v-model:visible="showEditModal"
      :route="editingRoute"
      @route-updated="handleRouteUpdated"
    />

    <!-- Language Switch Modal -->
    <n-modal
      v-model:show="showLanguageModal"
      preset="card"
      :title="t('settings.language')"
      style="width: 400px;"
      :bordered="false"
    >
      <n-space vertical :size="16">
        <n-text depth="3">{{ t('settings.languageDesc') }}</n-text>
        <n-radio-group :value="currentLocale" @update:value="switchLanguage">
          <n-space vertical>
            <n-radio value="zh-CN" size="large">
              🇨🇳 简体中文
            </n-radio>
            <n-radio value="en-US" size="large">
              🇺🇸 English
            </n-radio>
          </n-space>
        </n-radio-group>
      </n-space>
    </n-modal>

    <!-- Clear Stats Confirmation Dialog -->
    <n-modal
      v-model:show="showClearDialog"
      preset="dialog"
      :title="t('clearDialog.title')"
      type="error"
      :positive-text="t('clearDialog.confirm')"
      :negative-text="t('clearDialog.cancel')"
      @positive-click="confirmClearStats"
      @negative-click="showClearDialog = false"
    >
      <template #icon>
        <n-icon size="24" color="#e88080">
          <TrashIcon />
        </n-icon>
      </template>
      {{ t('clearDialog.message') }}
      <br>
      <br>
      <strong>{{ t('clearDialog.dataInclude') }}</strong>
      <ul>
        <li>{{ t('clearDialog.requestLogs') }}</li>
        <li>{{ t('clearDialog.tokenStats') }}</li>
        <li>{{ t('clearDialog.modelRanking') }}</li>
        <li>{{ t('clearDialog.heatmapData') }}</li>
      </ul>
    </n-modal>
  </n-config-provider>
</template>

<script setup>
import { ref, h, onMounted, computed, watch, nextTick } from 'vue'
import { useI18n } from 'vue-i18n'
import { darkTheme, NButton, NIcon, NTag, NSpace, NModal, NTooltip } from 'naive-ui'
import VChart from 'vue-echarts'
import { use } from 'echarts/core'
import { CanvasRenderer } from 'echarts/renderers'
import { LineChart } from 'echarts/charts'
import {
  TitleComponent,
  TooltipComponent,
  GridComponent,
} from 'echarts/components'
import {
  ServerOutline as ServerIcon,
  Moon as MoonIcon,
  Sunny as SunnyIcon,
  Add as AddIcon,
  GitNetwork as GitNetworkIcon,
  Cube as CubeIcon,
  StatsChart as StatsChartIcon,
  Flash as FlashIcon,
  ArrowForward as ArrowForwardIcon,
  Copy as CopyIcon,
  Refresh as RefreshIcon,
  CreateOutline as EditIcon,
  TrashOutline as DeleteIcon,
  Home as HomeIcon,
  List as ListIcon,
  BarChart as BarChartIcon,
  Settings as SettingsIcon,
  Location as LocationIcon,
  LogoGithub as LogoGithubIcon,
  InformationCircle as InformationCircleIcon,
  Code as CodeIcon,
  Link as LinkIcon,
  Trash as TrashIcon,
  Language as LanguageIcon,
} from '@vicons/ionicons5'
import AddRouteModal from './components/AddRouteModal.vue'
import EditRouteModal from './components/EditRouteModal.vue'

// 注册 ECharts 组件
use([
  CanvasRenderer,
  LineChart,
  TitleComponent,
  TooltipComponent,
  GridComponent,
])

// 使用全局 API（不需要 provider）
const showMessage = (type, content) => {
  if (window.$message) {
    window.$message[type](content)
  } else {
    console.log(`[${type}] ${content}`)
  }
}

// i18n
const { t, locale } = useI18n()

// Language
const showLanguageModal = ref(false)
const currentLocale = ref(localStorage.getItem('app-locale') || 'zh-CN')

const switchLanguage = (lang) => {
  locale.value = lang
  currentLocale.value = lang
  localStorage.setItem('app-locale', lang)
  showLanguageModal.value = false
  showMessage("success", t('messages.languageChanged'))
}

// Page State
const currentPage = ref('home') // 'home' | 'models' | 'stats' | 'settings'
const refreshing = ref(false)

// Theme
const isDark = ref(true)
const themeOverrides = {
  common: {
    primaryColor: '#18A058',
  },
}

const toggleTheme = () => {
  isDark.value = !isDark.value
  showMessage("info", isDark.value ? t('messages.switchedToDark') : t('messages.switchedToLight'))
}

// 刷新所有数据
const refreshAll = async () => {
  refreshing.value = true
  try {
    await Promise.all([
      loadRoutes(),
      loadStats(),
      loadConfig(),
      loadDailyStats(),
      loadHourlyStats(),
      loadModelRanking()
    ])
    showMessage("success", t('messages.dataRefreshed'))
  } catch (error) {
    showMessage("error", t('messages.refreshFailed') + ': ' + error)
  } finally {
    refreshing.value = false
  }
}

// Settings
const settings = ref({
  redirectKeyword: 'proxy_auto',
  autoStart: false,
  minimizeToTray: false,
  enableFileLog: false,
})

const updateRedirectKeyword = async () => {
  if (!window.go || !window.go.main || !window.go.main.App) {
    showMessage("error", t('messages.wailsNotReady'))
    return
  }
  try {
    await window.go.main.App.UpdateConfig(
      redirectConfig.value.enabled,
      settings.value.redirectKeyword,
      redirectConfig.value.targetModel
    )
    redirectConfig.value.keyword = settings.value.redirectKeyword
    showMessage("success", t('messages.redirectKeywordUpdated'))
    await loadConfig()
  } catch (error) {
    showMessage("error", t('messages.updateFailed') + ': ' + error)
  }
}

const saveSettings = () => {
  showMessage("info", t('messages.settingFailed'))
}

// 切换开机自启动
const toggleAutoStart = async (enabled) => {
  if (!window.go || !window.go.main || !window.go.main.App) {
    showMessage("error", t('messages.wailsNotReady'))
    return
  }
  try {
    await window.go.main.App.SetAutoStart(enabled)
    showMessage("success", enabled ? t('messages.autoStartEnabled') : t('messages.autoStartDisabled'))
  } catch (error) {
    showMessage("error", t('messages.settingFailed') + ': ' + error)
    settings.value.autoStart = !enabled // 恢复状态
  }
}

// 切换最小化到托盘
const toggleMinimizeToTray = async (enabled) => {
  if (!window.go || !window.go.main || !window.go.main.App) {
    showMessage("error", t('messages.wailsNotReady'))
    return
  }
  try {
    await window.go.main.App.SetMinimizeToTray(enabled)
    showMessage("success", enabled ? t('messages.minimizeEnabled') : t('messages.minimizeDisabled'))
  } catch (error) {
    showMessage("error", t('messages.settingFailed') + ': ' + error)
    settings.value.minimizeToTray = !enabled // 恢复状态
  }
}

// 切换文件日志
const toggleEnableFileLog = async (enabled) => {
  if (!window.go || !window.go.main || !window.go.main.App) {
    showMessage("error", t('messages.wailsNotReady'))
    return
  }
  try {
    await window.go.main.App.SetEnableFileLog(enabled)
    showMessage("success", enabled ? t('settings.fileLogEnabled') : t('settings.fileLogDisabled'))
  } catch (error) {
    showMessage("error", t('messages.settingFailed') + ': ' + error)
    settings.value.enableFileLog = !enabled // 恢复状态
  }
}

// Stats
const stats = ref({
  route_count: 0,
  model_count: 0,
  total_requests: 0,
  total_tokens: 0,
  today_tokens: 0, // 今日token使用量
  today_requests: 0, // 今日请求数
  success_rate: 0,
})

// 热力图数据
const heatmapData = ref([])

// 热力图 tooltip 状态
const heatmapTooltip = ref({
  show: false,
  x: 0,
  y: 0,
  date: '',
  tokens: 0,
  requests: 0
})

// 显示热力图 tooltip（使用固定定位避免被边框遮挡）
const showHeatmapTooltip = (event, day) => {
  const rect = event.target.getBoundingClientRect()
  heatmapTooltip.value = {
    show: true,
    x: rect.left + rect.width / 2,
    y: rect.top,
    date: day.date,
    tokens: day.tokens,
    requests: day.requests
  }
}

// 生成热力图数据结构（填充空白日期）
const generateHeatmapData = (dailyStats) => {
  const weeks = []
  const today = new Date()
  const statsMap = {}

  // 将统计数据转换为map（包含 tokens 和 requests）
  if (dailyStats && Array.isArray(dailyStats)) {
    dailyStats.forEach(stat => {
      statsMap[stat.date] = {
        tokens: stat.total_tokens || 0,
        requests: stat.requests || 0
      }
    })
  }

  // 计算起始日期（52周前的周日）
  const startDate = new Date(today)
  startDate.setDate(startDate.getDate() - 363) // 回到约52周前
  // 调整到周日
  const dayOfWeek = startDate.getDay()
  startDate.setDate(startDate.getDate() - dayOfWeek)

  // 生成53周的数据（确保覆盖完整一年）
  for (let i = 0; i < 53; i++) {
    const week = []
    for (let j = 0; j < 7; j++) {
      const date = new Date(startDate)
      date.setDate(date.getDate() + (i * 7 + j))
      // 使用本地日期格式
      const year = date.getFullYear()
      const month = String(date.getMonth() + 1).padStart(2, '0')
      const day = String(date.getDate()).padStart(2, '0')
      const dateStr = `${year}-${month}-${day}`
      const stat = statsMap[dateStr] || { tokens: 0, requests: 0 }
      week.push({
        date: dateStr,
        tokens: stat.tokens,
        requests: stat.requests
      })
    }
    weeks.push(week)
  }
  return weeks
}

// 动态计算月份标签（带位置信息）
const heatmapMonthsWithPosition = computed(() => {
  const monthsData = []
  const today = new Date()
  const startDate = new Date(today)
  startDate.setDate(startDate.getDate() - 363)
  // 调整到周日（与 generateHeatmapData 保持一致）
  const dayOfWeek = startDate.getDay()
  startDate.setDate(startDate.getDate() - dayOfWeek)
  
  // 使用 tm() 获取数组类型的翻译
  const monthNames = locale.value === 'zh-CN' 
    ? ['1月', '2月', '3月', '4月', '5月', '6月', '7月', '8月', '9月', '10月', '11月', '12月']
    : ['Jan', 'Feb', 'Mar', 'Apr', 'May', 'Jun', 'Jul', 'Aug', 'Sep', 'Oct', 'Nov', 'Dec']
  let lastMonth = -1
  
  // 遍历所有天数来检测月份变化
  for (let i = 0; i < 53; i++) {
    // 检查这一周的每一天，找到月份变化的位置
    for (let j = 0; j < 7; j++) {
      const date = new Date(startDate)
      date.setDate(date.getDate() + (i * 7 + j))
      const month = date.getMonth()
      if (month !== lastMonth) {
        // 如果是这周的第一天（周日）就是新月份开始，标记在这周
        // 否则标记在下一周
        const weekIndex = j === 0 ? i : (i < 52 ? i + 1 : i)
        // 避免重复添加同一个月
        if (monthsData.length === 0 || monthsData[monthsData.length - 1].name !== monthNames[month]) {
          monthsData.push({
            name: monthNames[month],
            weekIndex: j === 0 ? i : i
          })
        }
        lastMonth = month
        break // 找到这周的月份变化后跳出
      }
    }
  }
  return monthsData
})

const getHeatmapClass = (tokens) => {
  if (!tokens || tokens === 0) return 'level-0'
  if (tokens < 1000) return 'level-1'
  if (tokens < 5000) return 'level-2'
  if (tokens < 10000) return 'level-3'
  return 'level-4'
}

// 今日按小时统计数据
const hourlyStatsData = ref([])

// 今日折线图配置
const todayChartOption = computed(() => {
  // 生成24小时的数据（填充空白小时）
  const hourlyTokensMap = {}
  const hourlyRequestsMap = {}
  hourlyStatsData.value.forEach(stat => {
    hourlyTokensMap[stat.hour] = stat.total_tokens || 0
    hourlyRequestsMap[stat.hour] = stat.requests || 0
  })

  const hours = Array.from({ length: 24 }, (_, i) => `${i}:00`)
  const tokensData = Array.from({ length: 24 }, (_, i) => hourlyTokensMap[i] || 0)
  const requestsData = Array.from({ length: 24 }, (_, i) => hourlyRequestsMap[i] || 0)

  return {
    tooltip: {
      trigger: 'axis',
      formatter: function(params) {
        let result = params[0].axisValue + '<br/>'
        params.forEach(param => {
          // 对 Token 数量进行格式化
          let value = param.value
          if (param.seriesIndex === 0) { // Token 系列
            if (value >= 1000000) {
              value = (value / 1000000).toFixed(1) + 'M'
            } else if (value >= 1000) {
              value = (value / 1000).toFixed(1) + 'K'
            }
          }
          result += param.marker + param.seriesName + ': ' + value + '<br/>'
        })
        return result
      }
    },
    legend: {
      data: [t('stats.tokens'), t('stats.requestCount')],
      textStyle: {
        color: isDark.value ? '#fff' : '#333'
      }
    },
    grid: {
      left: '3%',
      right: '4%',
      bottom: '3%',
      containLabel: true
    },
    xAxis: {
      type: 'category',
      boundaryGap: false,
      data: hours
    },
    yAxis: [
      {
        type: 'value',
        name: 'Tokens',
        position: 'left',
        axisLabel: {
          formatter: function(value) {
            if (value >= 1000000) {
              return (value / 1000000).toFixed(1) + 'M'
            } else if (value >= 1000) {
              return (value / 1000).toFixed(1) + 'K'
            }
            return value
          }
        }
      },
      {
        type: 'value',
        name: t('stats.requestCount'),
        position: 'right'
      }
    ],
    series: [
      {
        name: t('stats.tokens'),
        type: 'line',
        smooth: true,
        data: tokensData,
        yAxisIndex: 0,
        areaStyle: {
          color: isDark.value ? 'rgba(24, 160, 88, 0.1)' : 'rgba(24, 160, 88, 0.2)'
        },
        lineStyle: {
          color: '#18a058'
        },
        itemStyle: {
          color: '#18a058'
        }
      },
      {
        name: t('stats.requestCount'),
        type: 'line',
        smooth: true,
        data: requestsData,
        yAxisIndex: 1,
        lineStyle: {
          color: '#f0a020'
        },
        itemStyle: {
          color: '#f0a020'
        }
      }
    ]
  }
})

// 接口使用排行数据
const modelRankingData = ref([])

const rankingColumns = computed(() => [
  { title: t('stats.rank'), key: 'rank', width: 80 },
  {
    title: t('stats.model'),
    key: 'model',
    render(row) {
      return h(NTag, { type: 'info' }, { default: () => row.model })
    }
  },
  { title: t('stats.requests'), key: 'requests' },
  {
    title: t('stats.totalTokensCol'),
    key: 'total_tokens',
    render(row) {
      return formatNumber(row.total_tokens || 0)
    }
  },
  {
    title: t('stats.successRate'),
    key: 'success_rate',
    render(row) {
      return `${row.success_rate || 0}%`
    }
  },
])

// Config
const config = ref({
  localApiKey: '',
  localApiEndpoint: '',
})

// Redirect Config
const redirectConfig = ref({
  enabled: false,
  keyword: 'proxy_auto',
  targetModel: '',
  targetName: '',
})

// Routes
const routes = ref([])
const showAddModal = ref(false)
const showEditModal = ref(false)
const editingRoute = ref(null)
const expandedGroups = ref([]) // 控制折叠面板展开状态
const fileInput = ref(null) // 文件输入引用
const showClearDialog = ref(false) // 清除数据确认对话框

// Computed: 按分组组织路由
const groupedRoutes = computed(() => {
  const groups = {}
  routes.value.forEach(route => {
    const groupName = route.group || '未分组'
    if (!groups[groupName]) {
      groups[groupName] = []
    }
    groups[groupName].push(route)
  })
  return groups
})


// 行属性设置
const rowProps = (row) => {
  return {
    'data-model': row.model
  }
}

// Pagination
const pagination = {
  pageSize: 10,
}

// 设置为重定向按钮处理
const setAsRedirect = async (model) => {
  redirectConfig.value.targetModel = model
  redirectConfig.value.enabled = true
  await saveRedirectConfig()
  showMessage("success", t('home.setRedirectSuccess'))
}

// 跳转到目标模型
const jumpToTargetModel = () => {
  currentPage.value = 'models'

  // 展开所有分组
  expandedGroups.value = Object.keys(groupedRoutes.value)

  // 等待DOM更新后滚动到目标模型
  nextTick(() => {
    // 查找目标模型所在的行
    const targetRows = document.querySelectorAll('[data-model="' + redirectConfig.value.targetModel + '"]')
    if (targetRows.length > 0) {
      targetRows[0].scrollIntoView({ behavior: 'smooth', block: 'center' })
    }
  })
}

// Table columns for home page
const columns = [
  {
    title: 'ID',
    key: 'id',
    width: 60,
  },
  {
    title: '名称',
    key: 'name',
    width: 150,
  },
  {
    title: '模型',
    key: 'model',
    width: 180,
    render(row) {
      return h(NTag, { type: 'info' }, { default: () => row.model })
    },
  },
  {
    title: 'API URL',
    key: 'api_url',
    ellipsis: {
      tooltip: true,
    },
  },
  {
    title: 'API Key',
    key: 'api_key',
    width: 150,
    render(row) {
      return maskApiKey(row.api_key)
    },
  },
  {
    title: '分组',
    key: 'group',
    width: 100,
    render(row) {
      return row.group ? h(NTag, { type: 'success', size: 'small' }, { default: () => row.group }) : '-'
    },
  },
  {
    title: '操作',
    key: 'actions',
    width: 150,
    render(row) {
      return h(NSpace, {}, {
        default: () => [
          h(
            NButton,
            {
              size: 'small',
              onClick: () => handleEdit(row),
            },
            { default: () => '编辑', icon: () => h(NIcon, {}, { default: () => h(EditIcon) }) }
          ),
          h(
            NButton,
            {
              size: 'small',
              type: 'error',
              onClick: () => handleDelete(row),
            },
            { default: () => '删除', icon: () => h(NIcon, {}, { default: () => h(DeleteIcon) }) }
          ),
        ]
      })
    },
  },
]

// Table columns for models page (with redirect button)
const modelsPageColumns = computed(() => [
  {
    title: 'ID',
    key: 'id',
    width: 60,
  },
  {
    title: t('models.name'),
    key: 'name',
    width: 150,
  },
  {
    title: t('models.model'),
    key: 'model',
    width: 200,
    render(row) {
      return h(NSpace, { align: 'center' }, {
        default: () => [
          h(NTag, { type: 'info' }, { default: () => row.model }),
          // 如果是当前重定向目标，显示标记
          redirectConfig.value.targetModel === row.model
            ? h(NTag, { type: 'success', size: 'small' }, { default: () => t('home.redirectTarget') })
            : null
        ]
      })
    },
  },
  {
    title: t('models.apiUrl'),
    key: 'api_url',
    ellipsis: {
      tooltip: true,
    },
  },
  {
    title: t('models.actions'),
    key: 'actions',
    width: 280,
    render(row) {
      return h(NSpace, {}, {
        default: () => [
          h(
            NButton,
            {
              size: 'small',
              onClick: () => handleEdit(row),
            },
            { default: () => t('models.edit'), icon: () => h(NIcon, {}, { default: () => h(EditIcon) }) }
          ),
          h(
            NButton,
            {
              size: 'small',
              type: 'error',
              onClick: () => handleDelete(row),
            },
            { default: () => t('models.delete'), icon: () => h(NIcon, {}, { default: () => h(DeleteIcon) }) }
          ),
          h(
            NButton,
            {
              size: 'small',
              type: 'primary',
              onClick: () => setAsRedirect(row.model),
            },
            { default: () => t('models.setAsTarget'), icon: () => h(NIcon, {}, { default: () => h(LinkIcon) }) }
          ),
        ]
      })
    },
  },
])

// Computed
const modelOptions = computed(() => {
  const models = routes.value.map(r => r.model)
  const uniqueModels = [...new Set(models)]
  return uniqueModels.map(m => ({ label: m, value: m }))
})

// Methods
const loadRoutes = async () => {
  try {
    if (!window.go || !window.go.main || !window.go.main.App) {
      console.error('Wails runtime not available')
      return
    }
    const data = await window.go.main.App.GetRoutes()
    routes.value = data || []
    console.log('Routes loaded:', routes.value.length)

    // 自动展开所有分组
    expandedGroups.value = Object.keys(groupedRoutes.value)
  } catch (error) {
    console.error('Failed to load routes:', error)
    showMessage("error", t('messages.refreshFailed') + ': ' + error)
  }
}

const loadStats = async () => {
  try {
    if (!window.go || !window.go.main || !window.go.main.App) {
      console.error('Wails runtime not available')
      return
    }
    const data = await window.go.main.App.GetStats()
    stats.value = data || stats.value
    console.log('Stats loaded:', stats.value)
  } catch (error) {
    console.error('加载统计失败:', error)
  }
}

// 加载每日统计（用于热力图）
const loadDailyStats = async () => {
  try {
    if (!window.go || !window.go.main || !window.go.main.App) {
      return
    }
    const data = await window.go.main.App.GetDailyStats(365) // 获取365天数据
    heatmapData.value = generateHeatmapData(data || [])
  } catch (error) {
    console.error('加载每日统计失败:', error)
  }
}

// 加载今日按小时统计（用于折线图）
const loadHourlyStats = async () => {
  try {
    if (!window.go || !window.go.main || !window.go.main.App) {
      return
    }
    const data = await window.go.main.App.GetHourlyStats()
    hourlyStatsData.value = data || []
  } catch (error) {
    console.error('加载按小时统计失败:', error)
  }
}

// 加载模型使用排行
const loadModelRanking = async () => {
  try {
    if (!window.go || !window.go.main || !window.go.main.App) {
      return
    }
    const data = await window.go.main.App.GetModelRanking(10) // 获取前10名
    modelRankingData.value = data || []
  } catch (error) {
    console.error('加载模型排行失败:', error)
  }
}

const loadConfig = async () => {
  try {
    if (!window.go || !window.go.main || !window.go.main.App) {
      console.error('Wails runtime not available')
      return
    }
    const data = await window.go.main.App.GetConfig()
    // 映射后端字段名到前端字段名
    config.value = {
      localApiKey: data.localApiKey || '',
      localApiEndpoint: data.openaiEndpoint || ''
    }
    redirectConfig.value.enabled = data.redirectEnabled || false
    redirectConfig.value.keyword = data.redirectKeyword || 'proxy_auto'
    redirectConfig.value.targetModel = data.redirectTargetModel || ''
    redirectConfig.value.targetName = data.redirectTargetName || ''
    settings.value.redirectKeyword = data.redirectKeyword || 'proxy_auto' // 同步到设置
    settings.value.minimizeToTray = data.minimizeToTray || false
    settings.value.autoStart = data.autoStart || false
    settings.value.enableFileLog = data.enableFileLog || false
    console.log('Config loaded:', config.value)
  } catch (error) {
    console.error('加载配置失败:', error)
  }
}

const saveRedirectConfig = async () => {
  if (!window.go || !window.go.main || !window.go.main.App) {
    showMessage("error", 'Wails 运行时未就绪')
    return
  }
  try {
    await window.go.main.App.UpdateConfig(
      redirectConfig.value.enabled,
      redirectConfig.value.keyword,
      redirectConfig.value.targetModel
    )
    showMessage("success", t('messages.redirectConfigSaved'))
    // 重新加载配置以获取最新的 targetName
    await loadConfig()
  } catch (error) {
    showMessage("error", t('messages.redirectConfigFailed') + ': ' + error)
  }
}

// 清理 API URL，移除末尾斜杠
const handleRouteAdded = () => {
  loadRoutes()
  loadStats()
}

const handleRouteUpdated = () => {
  loadRoutes()
  loadStats()
}

const handleEdit = (row) => {
  editingRoute.value = row
  showEditModal.value = true
}

const handleDelete = async (row) => {
  if (!window.go || !window.go.main || !window.go.main.App) {
    showMessage("error", 'Wails 运行时未就绪')
    return
  }
  try {
    await window.go.main.App.DeleteRoute(row.id)
    showMessage("success", t('deleteRoute.deleted'))
    loadRoutes()
    loadStats()
  } catch (error) {
    showMessage("error", t('deleteRoute.deleteFailed') + ': ' + error)
  }
}



const maskApiKey = (key) => {
  if (!key || key.length <= 10) return key
  return key.substring(0, 5) + '***' + key.substring(key.length - 5)
}

const copyToClipboard = async (text) => {
  try {
    await navigator.clipboard.writeText(text)
    showMessage("success", t('messages.copySuccess'))
  } catch (error) {
    showMessage("error", t('messages.copyFailed'))
  }
}

const formatNumber = (num) => {
  if (num >= 1000000) {
    return (num / 1000000).toFixed(1) + 'M'
  }
  if (num >= 1000) {
    return (num / 1000).toFixed(1) + 'K'
  }
  return num.toString()
}

// 生成随机 API Key
const generateRandomApiKey = () => {
  const chars = 'ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789'
  let result = 'sk-'
  for (let i = 0; i < 48; i++) {
    result += chars.charAt(Math.floor(Math.random() * chars.length))
  }
  return result
}

// 随机更新 API Key
const generateNewApiKey = async () => {
  if (!window.go || !window.go.main || !window.go.main.App) {
    showMessage("error", 'Wails 运行时未就绪')
    return
  }

  try {
    const newApiKey = generateRandomApiKey()
    await window.go.main.App.UpdateLocalApiKey(newApiKey)
    showMessage("success", 'API Key 已随机更新')
    await loadConfig() // 重新加载配置
  } catch (error) {
    showMessage("error", t('messages.updateFailed') + ': ' + error)
  }
}

// 导出路由为 JSON
const exportRoutes = () => {
  try {
    const exportData = routes.value.map(route => ({
      name: route.name,
      model: route.model,
      api_url: route.api_url,
      api_key: route.api_key,
      group: route.group,
    }))

    const jsonStr = JSON.stringify(exportData, null, 2)
    const blob = new Blob([jsonStr], { type: 'application/json' })
    const url = URL.createObjectURL(blob)
    const a = document.createElement('a')
    a.href = url
    a.download = `openai-router-routes-${new Date().toISOString().split('T')[0]}.json`
    document.body.appendChild(a)
    a.click()
    document.body.removeChild(a)
    URL.revokeObjectURL(url)

    showMessage("success", t('models.exportSuccess'))
  } catch (error) {
    showMessage("error", t('models.exportFailed') + ': ' + error)
  }
}

// 触发文件选择
const triggerImport = () => {
  fileInput.value?.click()
}

// 显示清除数据确认对话框
const showClearStatsDialog = () => {
  showClearDialog.value = true
}

// 确认清除统计数据
const confirmClearStats = async () => {
  if (!window.go || !window.go.main || !window.go.main.App) {
    showMessage("error", 'Wails 运行时未就绪')
    return
  }

  try {
    await window.go.main.App.ClearStats()
    showMessage("success", t('stats.clearSuccess'))
    showClearDialog.value = false

    // 重新加载数据
    await loadStats()
    await loadDailyStats()
    await loadHourlyStats()
    await loadModelRanking()
  } catch (error) {
    showMessage("error", t('stats.clearFailed') + ': ' + error)
  }
}

// 处理文件导入
const handleFileImport = async (event) => {
  const file = event.target.files?.[0]
  if (!file) return

  try {
    const text = await file.text()
    const importData = JSON.parse(text)

    if (!Array.isArray(importData)) {
      showMessage("error", 'JSON 格式错误：应为路由数组')
      return
    }

    if (!window.go || !window.go.main || !window.go.main.App) {
      showMessage("error", 'Wails 运行时未就绪')
      return
    }

    let successCount = 0
    let failCount = 0

    for (const route of importData) {
      try {
        await window.go.main.App.AddRoute(
          route.name || '',
          route.model || '',
          route.api_url || '',
          route.api_key || '',
          route.group || ''
        )
        successCount++
      } catch (error) {
        console.error('导入路由失败:', route, error)
        failCount++
      }
    }

    showMessage("success", t('models.importSuccess', { count: successCount }))
    loadRoutes()
    loadStats()
  } catch (error) {
    showMessage("error", t('models.importFailed') + ': ' + error)
  } finally {
    // 清空文件输入
    if (fileInput.value) {
      fileInput.value.value = ''
    }
  }
}

// Lifecycle
onMounted(async () => {
  // Wait for Wails runtime to be ready
  if (!window.go) {
    console.log('Waiting for Wails runtime...')
    await new Promise((resolve) => {
      const checkRuntime = setInterval(() => {
        if (window.go) {
          clearInterval(checkRuntime)
          resolve()
        }
      }, 100)
    })
  }

  console.log('Wails runtime ready, loading data...')
  loadRoutes()
  loadStats()
  loadConfig()
  loadDailyStats()
  loadHourlyStats()
  loadModelRanking()

  // 每 30 秒刷新一次统计
  setInterval(() => {
    loadStats()
    loadHourlyStats()
  }, 30000)

  // 每 5 分钟刷新一次热力图和排行
  setInterval(() => {
    loadDailyStats()
    loadModelRanking()
  }, 300000)
})

// Watch groupedRoutes to automatically expand all groups when they change
watch(groupedRoutes, (newGroups) => {
  console.log('Grouped routes changed, expanding all groups')
  expandedGroups.value = Object.keys(newGroups)
}, { deep: true })
</script>

<style>
/* 全局滚动条隐藏 - Wails 专用 */
:deep(*)::-webkit-scrollbar {
  width: 0px !important;
  height: 0px !important;
  background: transparent !important;
  display: none !important;
}

:deep(*) {
  scrollbar-width: none !important;
  -ms-overflow-style: none !important;
}

/* 针对 Naive UI 组件的特殊处理 */
:deep(.n-layout-content) {
  overflow-y: auto !important;
  overflow-x: hidden !important;
}

:deep(.n-layout-content::-webkit-scrollbar),
:deep(.n-data-table::-webkit-scrollbar),
:deep(.n-card::-webkit-scrollbar),
:deep(.n-scrollbar::-webkit-scrollbar),
:deep(.n-collapse-item::-webkit-scrollbar),
:deep(.n-tab-pane::-webkit-scrollbar) {
  width: 0px !important;
  height: 0px !important;
  background: transparent !important;
  display: none !important;
}
</style>

<style scoped>
:deep(.n-card__content) {
  padding: 16px;
}

:deep(.n-statistic) {
  color: white;
}

:deep(.n-statistic .n-statistic__label) {
  color: rgba(255, 255, 255, 0.9);
  font-size: 14px;
}

:deep(.n-statistic .n-statistic__value) {
  color: white;
  font-size: 28px;
  font-weight: 600;
}



/* GitHub 热力图样式 - 全屏版本 */
.heatmap-container {
  padding: 20px;
  position: relative;
  width: 100%;
  overflow-x: auto;
}

.heatmap-months-row {
  position: relative;
  height: 20px;
  margin-bottom: 8px;
  font-size: 12px;
  color: #888;
  width: 100%;
}

.heatmap-month-label {
  position: absolute;
  white-space: nowrap;
  transform: translateX(0);
}

.heatmap-grid {
  display: flex;
  gap: 4px;
  margin-bottom: 12px;
  width: 100%;
  justify-content: flex-start;
  overflow-x: auto;
}

.heatmap-week {
  display: flex;
  flex-direction: column;
  gap: 4px;
  flex: 0 0 auto;
  width: calc((100% - 52 * 4px) / 53);
  min-width: 12px;
}

.heatmap-cell {
  width: 100%;
  aspect-ratio: 1;
  min-width: 10px;
  max-width: 16px;
  border-radius: 2px;
  cursor: pointer;
  transition: all 0.2s;
}

.heatmap-cell:hover {
  transform: scale(1.5);
  border: 1px solid #fff;
  z-index: 10;
}

.heatmap-cell.level-0 {
  background-color: #3a3a3a;
}

.heatmap-cell.level-1 {
  background-color: #9be9a8;
}

.heatmap-cell.level-2 {
  background-color: #40c463;
}

.heatmap-cell.level-3 {
  background-color: #30a14e;
}

.heatmap-cell.level-4 {
  background-color: #216e39;
}

.heatmap-tooltip {
  position: fixed;
  background: rgba(0, 0, 0, 0.85);
  color: #fff;
  padding: 8px 12px;
  border-radius: 6px;
  font-size: 12px;
  pointer-events: none;
  z-index: 1000;
  white-space: nowrap;
  box-shadow: 0 2px 8px rgba(0, 0, 0, 0.3);
  transform: translate(-50%, -100%);
  margin-top: -10px;
}

.heatmap-legend {
  display: flex;
  align-items: center;
  gap: 4px;
  font-size: 12px;
  color: #888;
  justify-content: flex-end;
  margin-top: 8px;
}

.legend-box {
  width: 11px;
  height: 11px;
  border-radius: 2px;
}

.legend-box.level-0 {
  background-color: #3a3a3a;
}

.legend-box.level-1 {
  background-color: #9be9a8;
}

.legend-box.level-2 {
  background-color: #40c463;
}

.legend-box.level-3 {
  background-color: #30a14e;
}

.legend-box.level-4 {
  background-color: #216e39;
}
</style>
