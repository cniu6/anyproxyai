<template>
  <n-modal
    v-model:show="showModal"
    preset="card"
    :title="t('addRoute.title')"
    style="width: 600px;"
    :mask-closable="false"
    @after-leave="resetForm"
  >
    <n-form
      ref="formRef"
      :model="formModel"
      :rules="formRules"
      label-placement="left"
      label-width="100px"
    >
      <n-form-item :label="t('addRoute.routeBaseName')" path="baseName">
        <n-input v-model:value="formModel.baseName" :placeholder="t('addRoute.routeBaseNamePlaceholder')" />
        <template #feedback>
          <span style="color: #888; font-size: 12px;">{{ t('addRoute.routeBaseNameTip') }}</span>
        </template>
      </n-form-item>

      <n-form-item :label="t('addRoute.modelId')" path="model">
        <n-space style="width: 100%;" vertical>
          <n-input
            :value="modelDisplayValue"
            :placeholder="t('addRoute.modelIdPlaceholder')"
            readonly
            style="flex: 1;"
          />
          <n-space>
            <n-button @click="fetchModels" :loading="fetchingModels">
              {{ t('addRoute.fetchModels') }}
            </n-button>
            <n-text depth="3" style="font-size: 12px;">{{ t('addRoute.multiSelectTip') }}</n-text>
          </n-space>
        </n-space>
      </n-form-item>

      <n-form-item :label="t('addRoute.manualInput')" path="manualInput">
        <n-space style="width: 100%;" vertical>
          <n-input
            v-model:value="manualInputValue"
            :placeholder="t('addRoute.manualInputPlaceholder')"
            type="textarea"
            :autosize="{ minRows: 2, maxRows: 4 }"
          />
          <n-space>
            <n-button type="primary" size="small" @click="addManualModels">
              {{ t('addRoute.addModels') }}
            </n-button>
            <n-text depth="3" style="font-size: 12px;">{{ t('addRoute.manualInputTip') }}</n-text>
          </n-space>
        </n-space>
      </n-form-item>

      <n-form-item :label="t('addRoute.apiUrl')" path="apiUrl">
        <n-input
          v-model:value="formModel.apiUrl"
          :placeholder="t('addRoute.apiUrlPlaceholder')"
          @blur="cleanApiUrl"
        />
        <template #feedback>
          <span style="color: #888; font-size: 12px;">{{ t('addRoute.apiUrlTip') }}</span>
        </template>
      </n-form-item>

      <n-form-item :label="t('addRoute.apiKey')" path="apiKey">
        <n-input v-model:value="formModel.apiKey" type="password" :placeholder="t('addRoute.apiKeyPlaceholder')" show-password-on="click" />
      </n-form-item>

      <n-form-item :label="t('addRoute.group')" path="group">
        <n-input v-model:value="formModel.group" :placeholder="t('addRoute.groupPlaceholder')" />
      </n-form-item>

      <n-form-item :label="t('addRoute.apiFormat')" path="format">
        <n-select
          v-model:value="formModel.format"
          :options="formatOptions"
          :placeholder="t('addRoute.apiFormatPlaceholder')"
          @update:value="onFormatChange"
        />
        <template #feedback>
          <span style="color: #888; font-size: 12px;">{{ t('addRoute.apiFormatTip') }}</span>
        </template>
      </n-form-item>
    </n-form>

    <template #footer>
      <n-space justify="end">
        <n-button @click="closeModal">{{ t('addRoute.cancel') }}</n-button>
        <n-button type="primary" @click="handleSubmit" :loading="submitting">
          {{ t('addRoute.add') }}
        </n-button>
      </n-space>
    </template>
  </n-modal>

  <!-- Model Select Modal -->
  <n-modal
    v-model:show="showModelSelectModal"
    preset="card"
    :title="'🎯 ' + t('addRoute.selectModel')"
    style="width: 800px; max-height: 600px;"
  >
    <n-input
      v-model:value="modelSearchKeyword"
      :placeholder="t('addRoute.searchModel')"
      clearable
      style="margin-bottom: 16px;"
    />
    <n-scrollbar style="max-height: 450px;">
      <n-grid :x-gap="12" :y-gap="12" :cols="2">
        <n-grid-item
          v-for="model in filteredModels"
          :key="model"
        >
          <n-card
            :title="model"
            hoverable
            @click="toggleModelSelection(model)"
            style="cursor: pointer; transition: all 0.3s;"
            :class="{'selected-model-card': isModelSelected(model)}"
          >
            <template #header>
              <n-ellipsis style="max-width: 100%;" :tooltip="{ width: 300 }">
                <n-space align="center" justify="space-between">
                  <n-text strong>{{ model }}</n-text>
                  <n-checkbox
                    :checked="isModelSelected(model)"
                    @update:checked="() => toggleModelSelection(model)"
                    @click.stop
                  />
                </n-space>
              </n-ellipsis>
            </template>
            <n-space vertical size="small">
              <n-tag :type="getModelTagType(model)" size="small">
                {{ getModelProvider(model) }}
              </n-tag>
              <n-text depth="3" style="font-size: 12px;">
                {{ t('addRoute.clickToToggle') }}
              </n-text>
            </n-space>
          </n-card>
        </n-grid-item>
      </n-grid>
      <n-empty
        v-if="filteredModels.length === 0"
        :description="t('addRoute.noModelFound')"
        style="margin: 60px 0;"
      />
    </n-scrollbar>
    <template #footer>
      <n-space justify="space-between" align="center">
        <n-space vertical>
          <n-text depth="3">{{ t('addRoute.totalModels', { count: fetchedModels.length }) }}</n-text>
          <n-text depth="3" v-if="formModel.models.length > 0">
            {{ t('addRoute.selectedCount', { count: formModel.models.length }) }}
          </n-text>
        </n-space>
        <n-space>
          <n-button @click="clearModelSelection" v-if="formModel.models.length > 0">
            {{ t('addRoute.clearSelection') }}
          </n-button>
          <n-button type="primary" @click="confirmModelSelection">
            {{ t('addRoute.confirm') }}
          </n-button>
        </n-space>
      </n-space>
    </template>
  </n-modal>
</template>

<script setup>
import { ref, computed, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { NTag } from 'naive-ui'

const { t } = useI18n()

// Props
const props = defineProps({
  visible: {
    type: Boolean,
    default: false
  }
})

// Emits
const emit = defineEmits(['update:visible', 'route-added'])

// Refs
const formRef = ref(null)
const showModal = ref(props.visible)
const showModelSelectModal = ref(false)
const submitting = ref(false)
const fetchingModels = ref(false)
const fetchedModels = ref([])
const modelSearchKeyword = ref('')
const manualInputValue = ref('')

// Form model
const formModel = ref({
  baseName: '', // 路由基础名称
  model: '',
  models: [], // 多选模型列表
  apiUrl: '',
  apiKey: '',
  group: '',
  format: 'openai', // 默认格式
})

// Form rules (computed for i18n)
const formRules = computed(() => ({
  baseName: { required: true, message: t('addRoute.routeBaseNamePlaceholder') },
  model: { required: true, message: t('addRoute.modelIdPlaceholder') },
  apiUrl: { required: true, message: t('addRoute.apiUrlPlaceholder') },
  format: { required: true, message: t('addRoute.apiFormatPlaceholder') },
}))

// Format options (computed for i18n)
const formatOptions = computed(() => [
  { label: t('addRoute.openaiFormat'), value: 'openai' },
  { label: t('addRoute.claudeFormat'), value: 'claude' },
  { label: t('addRoute.geminiFormat'), value: 'gemini', disabled: true },
])

// Format conversion state
const showFormatConversion = ref(false)
const conversionPreview = ref(null)

// Watch for visibility changes
watch(() => props.visible, (newVal) => {
  showModal.value = newVal
})

// Watch for modal show changes
watch(showModal, (newVal) => {
  emit('update:visible', newVal)
})

// Computed: filtered models based on search
const filteredModels = computed(() => {
  if (!modelSearchKeyword.value) {
    return fetchedModels.value
  }
  const keyword = modelSearchKeyword.value.toLowerCase()
  return fetchedModels.value.filter(model =>
    model.toLowerCase().includes(keyword)
  )
})

// Computed: display all selected models as comma-separated string
const modelDisplayValue = computed(() => {
  if (formModel.value.models.length > 1) {
    return formModel.value.models.join(', ')
  } else if (formModel.value.models.length === 1) {
    return formModel.value.models[0]
  }
  return formModel.value.model || ''
})

// Methods
const closeModal = () => {
  showModal.value = false
}

const resetForm = () => {
  formModel.value = {
    baseName: '',
    model: '',
    models: [],
    apiUrl: '',
    apiKey: '',
    group: '',
    format: 'openai',
  }
  manualInputValue.value = ''
  showFormatConversion.value = false
  conversionPreview.value = null
  formRef.value?.restoreValidation()
}

const cleanApiUrl = () => {
  if (formModel.value.apiUrl) {
    // 只做 trim，不再自动移除末尾斜杠
    // 如果末尾有斜杠，表示用户希望直接使用该路径（如 /v4/chat/completions 而非 /v4/v1/chat/completions）
    const trimmed = formModel.value.apiUrl.trim()
    if (trimmed !== formModel.value.apiUrl) {
      formModel.value.apiUrl = trimmed
    }
  }
}

// 添加手动输入的模型
const addManualModels = () => {
  if (!manualInputValue.value || manualInputValue.value.trim() === '') {
    window.$message?.warning(t('addRoute.enterModelNames'))
    return
  }

  // 解析用户输入的模型列表（支持逗号、空格、换行分隔）
  const models = manualInputValue.value
    .split(/[,\s\n]+/)
    .map(m => m.trim())
    .filter(m => m.length > 0)

  if (models.length === 0) {
    window.$message?.warning(t('addRoute.enterModelNames'))
    return
  }

  // 去重并合并到现有模型列表
  const oldLength = formModel.value.models.length
  const modelSet = new Set([...formModel.value.models, ...models])
  const uniqueModels = Array.from(modelSet)
  const addedCount = uniqueModels.length - oldLength

  // 使用 splice 确保响应式更新
  formModel.value.models.splice(0, formModel.value.models.length, ...uniqueModels)
  formModel.value.model = uniqueModels[0]

  // 清空输入框
  manualInputValue.value = ''

  // 触发格式转换预览
  updateFormatConversion()

  if (addedCount > 0) {
    window.$message?.success(t('addRoute.modelsAdded', { count: addedCount }))
  } else {
    window.$message?.info(t('addRoute.noNewModels'))
  }
}

const fetchModels = async () => {
  if (!formModel.value.apiUrl) {
    window.$message?.warning(t('addRoute.enterApiUrlFirst'))
    return
  }

  // 检查 Wails 运行时
  if (!window.go || !window.go.main || !window.go.main.App) {
    window.$message?.error(t('addRoute.wailsNotReady'))
    return
  }

  fetchingModels.value = true
  try {
    const models = await window.go.main.App.FetchRemoteModels(
      formModel.value.apiUrl,
      formModel.value.apiKey || ''
    )
    fetchedModels.value = models
    showModelSelectModal.value = true
  } catch (error) {
    window.$message?.error(t('addRoute.fetchFailed') + ': ' + error)
  } finally {
    fetchingModels.value = false
  }
}

const selectModel = (model) => {
  formModel.value.model = model
  showModelSelectModal.value = false
  modelSearchKeyword.value = '' // 清空搜索
  window.$message?.success(t('addRoute.modelSelected') + ': ' + model)
  // 触发格式转换预览
  updateFormatConversion()
}

// 判断模型是否被选中
const isModelSelected = (model) => {
  return formModel.value.models.includes(model)
}

// 切换模型选择状态
const toggleModelSelection = (model) => {
  const index = formModel.value.models.indexOf(model)
  if (index > -1) {
    formModel.value.models.splice(index, 1)
  } else {
    formModel.value.models.push(model)
  }
}

// 确认模型选择
const confirmModelSelection = () => {
  if (formModel.value.models.length === 0) {
    window.$message?.warning(t('addRoute.selectAtLeastOne'))
    return
  }
  // 将选中的模型列表保存到 models 字段（不合并）
  formModel.value.model = formModel.value.models[0] // 显示第一个
  showModelSelectModal.value = false
  modelSearchKeyword.value = ''
  window.$message?.success(t('addRoute.modelsSelected') + ': ' + formModel.value.models.length + ' ' + t('addRoute.models'))
  updateFormatConversion()
}

// 清空模型选择
const clearModelSelection = () => {
  formModel.value.models = []
}

// 格式变化处理
const onFormatChange = (format) => {
  updateFormatConversion()
}

// 更新格式转换预览
const updateFormatConversion = () => {
  if (!formModel.value.model || !formModel.value.apiUrl || formModel.value.format === 'openai') {
    showFormatConversion.value = false
    conversionPreview.value = null
    return
  }

  const originalFormat = detectOriginalFormat()
  if (originalFormat !== formModel.value.format) {
    const preview = generateFormatPreview(originalFormat, formModel.value.format)
    showFormatConversion.value = true
    conversionPreview.value = preview
  } else {
    showFormatConversion.value = false
    conversionPreview.value = null
  }
}

// 检测原始格式
const detectOriginalFormat = () => {
  const url = formModel.value.apiUrl.toLowerCase()
  const model = formModel.value.model.toLowerCase()

  if (url.includes('api.openai.com') || model.startsWith('gpt-') || model.startsWith('o1-')) {
    return 'openai'
  } else if (url.includes('api.anthropic.com') || model.startsWith('claude-')) {
    return 'claude'
  } else if (url.includes('generativelanguage.googleapis.com') || model.startsWith('gemini-')) {
    return 'gemini'
  }
  return 'openai' // 默认
}

// 生成格式转换预览
const generateFormatPreview = (fromFormat, toFormat) => {
  const model = formModel.value.model
  const url = formModel.value.apiUrl

  const urlMappings = {
    'openai': {
      'claude': 'https://api.anthropic.com/v1',
      'gemini': 'https://generativelanguage.googleapis.com/v1'
    },
    'claude': {
      'openai': 'https://api.openai.com/v1',
      'gemini': 'https://generativelanguage.googleapis.com/v1'
    },
    'gemini': {
      'openai': 'https://api.openai.com/v1',
      'claude': 'https://api.anthropic.com/v1'
    }
  }

  const modelMappings = {
    'openai': {
      'claude': {
        'gpt-4-turbo': 'claude-3-5-sonnet-20241022',
        'gpt-4': 'claude-3-sonnet-20240229',
        'gpt-3.5-turbo': 'claude-3-haiku-20240307',
        'o1-preview': 'claude-3-opus-20240229',
        'o1-mini': 'claude-3-sonnet-20240229'
      },
      'gemini': {
        'gpt-4-turbo': 'gemini-1.5-pro',
        'gpt-4': 'gemini-1.0-pro',
        'gpt-3.5-turbo': 'gemini-1.5-flash',
        'gpt-4-vision-preview': 'gemini-pro-vision'
      }
    },
    'claude': {
      'openai': {
        'claude-3-opus-20240229': 'gpt-4-turbo',
        'claude-3-sonnet-20240229': 'gpt-4',
        'claude-3-haiku-20240307': 'gpt-3.5-turbo',
        'claude-3-5-sonnet-20241022': 'gpt-4-turbo'
      },
      'gemini': {
        'claude-3-opus-20240229': 'gemini-1.5-pro',
        'claude-3-sonnet-20240229': 'gemini-1.0-pro',
        'claude-3-haiku-20240307': 'gemini-1.5-flash',
        'claude-3-5-sonnet-20241022': 'gemini-1.5-pro'
      }
    },
    'gemini': {
      'openai': {
        'gemini-1.5-pro': 'gpt-4-turbo',
        'gemini-1.0-pro': 'gpt-4',
        'gemini-1.5-flash': 'gpt-3.5-turbo',
        'gemini-pro-vision': 'gpt-4-vision-preview'
      },
      'claude': {
        'gemini-1.5-pro': 'claude-3-5-sonnet-20241022',
        'gemini-1.0-pro': 'claude-3-sonnet-20240229',
        'gemini-1.5-flash': 'claude-3-haiku-20240307'
      }
    }
  }

  const newUrl = urlMappings[fromFormat]?.[toFormat] || url
  const newModel = modelMappings[fromFormat]?.[toFormat]?.[model] || getDefaultModel(toFormat)

  return {
    url: newUrl,
    model: newModel
  }
}

// 获取默认模型
const getDefaultModel = (format) => {
  const defaults = {
    'openai': 'gpt-3.5-turbo',
    'claude': 'claude-3-sonnet-20240229',
    'gemini': 'gemini-1.5-pro'
  }
  return defaults[format] || 'gpt-3.5-turbo'
}

const handleSubmit = async () => {
  if (!window.go || !window.go.main || !window.go.main.App) {
    window.$message?.error(t('addRoute.wailsNotReady'))
    return
  }

  try {
    await formRef.value?.validate()
    submitting.value = true

    // 只做 trim，保留末尾斜杠（如果有的话，表示用户希望直接使用该路径）
    const cleanedApiUrl = formModel.value.apiUrl.trim()

    // 获取模型列表
    const selectedModels = formModel.value.models.length > 0
      ? formModel.value.models
      : (formModel.value.model ? [formModel.value.model] : [])

    if (selectedModels.length === 0) {
      window.$message?.warning(t('addRoute.selectAtLeastOne'))
      return
    }

    // 路由名称直接使用 baseName（不再拼接模型名）
    const routeName = formModel.value.baseName || 'Route'
    let successCount = 0
    let failCount = 0

    for (const model of selectedModels) {
      try {
        await window.go.main.App.AddRoute(
          routeName,
          model,
          cleanedApiUrl,
          formModel.value.apiKey,
          formModel.value.group,
          formModel.value.format
        )
        successCount++
      } catch (error) {
        console.error('Failed to add route for model:', model, error)
        failCount++
      }
    }

    if (successCount > 0) {
      window.$message?.success(t('addRoute.routesAdded', { count: successCount }))
    }
    if (failCount > 0) {
      window.$message?.warning(`${failCount} ${t('addRoute.operationFailed')}`)
    }

    emit('route-added')
    closeModal()
  } catch (error) {
    if (error.errors) {
      // 表单验证错误
      return
    }
    window.$message?.error(t('addRoute.operationFailed') + ': ' + error)
  } finally {
    submitting.value = false
  }
}

// 根据模型名称识别提供商
const getModelProvider = (model) => {
  const lowerModel = model.toLowerCase()
  if (lowerModel.includes('gpt') || lowerModel.includes('openai')) return 'OpenAI'
  if (lowerModel.includes('claude')) return 'Anthropic'
  if (lowerModel.includes('gemini')) return 'Google'
  if (lowerModel.includes('deepseek')) return 'DeepSeek'
  if (lowerModel.includes('glm') || lowerModel.includes('chatglm')) return '智谱AI'
  if (lowerModel.includes('qwen') || lowerModel.includes('通义')) return '阿里云'
  if (lowerModel.includes('ernie') || lowerModel.includes('文心')) return '百度'
  if (lowerModel.includes('spark') || lowerModel.includes('讯飞')) return '讯飞'
  if (lowerModel.includes('llama')) return 'Meta'
  if (lowerModel.includes('mistral')) return 'Mistral'
  return t('common.other')
}

// 根据提供商返回标签颜色
const getModelTagType = (model) => {
  const provider = getModelProvider(model)
  const typeMap = {
    'OpenAI': 'success',
    'Anthropic': 'info',
    'Google': 'warning',
    'DeepSeek': 'error',
    '智谱AI': 'primary',
    '阿里云': 'default',
    '百度': 'info',
    '讯飞': 'success',
    'Meta': 'warning',
    'Mistral': 'error'
  }
  return typeMap[provider] || 'default'
}
</script>

<style scoped>
.selected-model-card {
  border: 2px solid #18a058 !important;
  box-shadow: 0 0 10px rgba(24, 160, 88, 0.3) !important;
}

.selected-model-card:hover {
  transform: translateY(-2px);
  box-shadow: 0 4px 16px rgba(24, 160, 88, 0.4) !important;
}
</style>