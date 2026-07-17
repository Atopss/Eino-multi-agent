import { describe, it, expect } from 'vitest'
import { mount } from '@vue/test-utils'
import StatePanel from './StatePanel.vue'

describe('StatePanel', () => {
  it('渲染标题与描述', () => {
    const w = mount(StatePanel, {
      props: { kind: 'empty', title: '暂无数据', description: '稍后重试' },
    })
    expect(w.text()).toContain('暂无数据')
    expect(w.text()).toContain('稍后重试')
  })

  it('error 类型使用危险色', () => {
    const w = mount(StatePanel, { props: { kind: 'error', title: '出错了' } })
    expect(w.find('.text-danger').exists()).toBe(true)
  })

  it('loading 类型展示旋转图标', () => {
    const w = mount(StatePanel, { props: { kind: 'loading' } })
    expect(w.find('.animate-spin').exists()).toBe(true)
  })

  it('可插入 action 插槽', () => {
    const w = mount(StatePanel, {
      slots: { action: '<button>重试</button>' },
    })
    expect(w.find('button').text()).toBe('重试')
  })
})
