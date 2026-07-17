import { describe, it, expect } from 'vitest'
import { mount } from '@vue/test-utils'
import Onboarding from './Onboarding.vue'

describe('Onboarding', () => {
  it('展示三步引导', () => {
    const w = mount(Onboarding)
    expect(w.text()).toContain('建一个智能体')
    expect(w.text()).toContain('喂点本地资料')
    expect(w.text()).toContain('开聊并看引用')
  })

  it('点击「创建第一个智能体」触发 create-agent', async () => {
    const w = mount(Onboarding)
    await w.findAll('button')[0].trigger('click')
    expect(w.emitted('create-agent')).toBeTruthy()
  })

  it('点击「稍后再说」触发 dismiss', async () => {
    const w = mount(Onboarding)
    await w.findAll('button')[1].trigger('click')
    expect(w.emitted('dismiss')).toBeTruthy()
  })
})
