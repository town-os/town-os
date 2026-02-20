import { describe, it, expect } from 'vitest'
import { renderHook, act } from '@testing-library/react'
import { useFollowMode } from './use-follow-mode.js'

describe('useFollowMode', () => {
  it('starts with followMode=true', () => {
    const { result } = renderHook(() => useFollowMode(false))
    expect(result.current[0]).toBe(true)
  })

  it('disables follow when search becomes active', () => {
    const { result, rerender } = renderHook(
      ({ active }) => useFollowMode(active),
      { initialProps: { active: false } },
    )
    expect(result.current[0]).toBe(true)

    rerender({ active: true })
    expect(result.current[0]).toBe(false)
  })

  it('restores follow when search is cleared after being active', () => {
    const { result, rerender } = renderHook(
      ({ active }) => useFollowMode(active),
      { initialProps: { active: false } },
    )
    expect(result.current[0]).toBe(true)

    rerender({ active: true })
    expect(result.current[0]).toBe(false)

    rerender({ active: false })
    expect(result.current[0]).toBe(true)
  })

  it('restores followMode=false if it was off before search', () => {
    const { result, rerender } = renderHook(
      ({ active }) => useFollowMode(active),
      { initialProps: { active: false } },
    )

    // Turn off follow manually.
    act(() => { result.current[1](false) })
    expect(result.current[0]).toBe(false)

    // Search activates — still false.
    rerender({ active: true })
    expect(result.current[0]).toBe(false)

    // Search clears — restores to false (was false before search).
    rerender({ active: false })
    expect(result.current[0]).toBe(false)
  })

  it('toggleFollow flips follow mode', () => {
    const { result } = renderHook(() => useFollowMode(false))
    expect(result.current[0]).toBe(true)

    act(() => { result.current[2]() })
    expect(result.current[0]).toBe(false)

    act(() => { result.current[2]() })
    expect(result.current[0]).toBe(true)
  })

  it('does not save again on repeated search-active rerenders', () => {
    const { result, rerender } = renderHook(
      ({ active }) => useFollowMode(active),
      { initialProps: { active: false } },
    )
    expect(result.current[0]).toBe(true)

    // Search activates.
    rerender({ active: true })
    expect(result.current[0]).toBe(false)

    // Additional rerenders while still active should not overwrite the saved state.
    rerender({ active: true })
    rerender({ active: true })
    expect(result.current[0]).toBe(false)

    // Clear search — should restore the original true.
    rerender({ active: false })
    expect(result.current[0]).toBe(true)
  })

  it('handles multiple search on/off cycles', () => {
    const { result, rerender } = renderHook(
      ({ active }) => useFollowMode(active),
      { initialProps: { active: false } },
    )
    expect(result.current[0]).toBe(true)

    // First cycle: search on then off.
    rerender({ active: true })
    expect(result.current[0]).toBe(false)
    rerender({ active: false })
    expect(result.current[0]).toBe(true)

    // Manually turn off follow.
    act(() => { result.current[1](false) })
    expect(result.current[0]).toBe(false)

    // Second cycle: search on then off — restores false.
    rerender({ active: true })
    expect(result.current[0]).toBe(false)
    rerender({ active: false })
    expect(result.current[0]).toBe(false)
  })
})
