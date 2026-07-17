import { describe, it, expect } from 'vitest'
import { mount } from '@vue/test-utils'
import AppButton from './AppButton.vue'

describe('AppButton', () => {
  it('默认渲染 primary 变体', () => {
    const w = mount(AppButton, { slots: { default: '确定' } })
    expect(w.classes()).toContain('btn-primary')
    expect(w.text()).toBe('确定')
  })

  it('outline / ghost 变体正确切换', () => {
    expect(mount(AppButton, { props: { variant: 'outline' } }).classes()).toContain('btn-outline')
    expect(mount(AppButton, { props: { variant: 'ghost' } }).classes()).toContain('btn-ghost')
  })

  it('loading 时禁用并展示加载图标', () => {
    const w = mount(AppButton, { props: { loading: true } })
    expect(w.attributes('disabled')).toBeDefined()
    expect(w.find('.animate-spin').exists()).toBe(true)
  })

  it('block 时占满宽度', () => {
    const w = mount(AppButton, { props: { block: true } })
    expect(w.classes()).toContain('w-full')
  })
})
