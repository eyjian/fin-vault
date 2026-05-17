<script setup lang="ts">
// MoneyInput：以字符串绑定，避免 JS number 精度损失。
// 仅允许：可选负号、数字、最多 1 个小数点。

import { computed } from 'vue'

interface Props {
  modelValue?: string
  placeholder?: string
  disabled?: boolean
  precision?: number // 显示小数位（仅 blur 时格式化），不限制输入
  prefix?: string
  size?: 'large' | 'default' | 'small'
}

const props = withDefaults(defineProps<Props>(), {
  modelValue: '',
  placeholder: '0.00',
  disabled: false,
  precision: 0,
  prefix: '',
  size: 'default'
})

const emit = defineEmits<{ (e: 'update:modelValue', v: string): void }>()

const valueRef = computed({
  get: () => props.modelValue ?? '',
  set: (v: string) => emit('update:modelValue', v)
})

function onInput(raw: string) {
  // 只允许 数字 / 小数点 / 负号
  let v = raw.replace(/[^\d.-]/g, '')
  // 仅允许一个负号且必须在开头
  if (v.includes('-')) {
    const neg = v.startsWith('-')
    v = (neg ? '-' : '') + v.replace(/-/g, '')
  }
  // 仅允许一个小数点
  const idx = v.indexOf('.')
  if (idx !== -1) {
    v = v.slice(0, idx + 1) + v.slice(idx + 1).replace(/\./g, '')
  }
  valueRef.value = v
}
</script>

<template>
  <el-input
    :model-value="valueRef"
    :placeholder="placeholder"
    :disabled="disabled"
    :size="size"
    @input="onInput"
  >
    <template v-if="prefix" #prepend>{{ prefix }}</template>
  </el-input>
</template>
