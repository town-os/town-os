import { describe, it, expect } from 'vitest'
import { render, screen, fireEvent } from '@testing-library/react'
import { JsonTree } from './json-tree.jsx'

describe('JsonTree', () => {
  it('renders "No details" for empty string', () => {
    render(<JsonTree data="" />)
    expect(screen.getByText('No details')).toBeTruthy()
  })

  it('renders "No details" for null', () => {
    render(<JsonTree data={null} />)
    expect(screen.getByText('No details')).toBeTruthy()
  })

  it('renders raw text for invalid JSON', () => {
    render(<JsonTree data="not json" />)
    expect(screen.getByText('not json')).toBeTruthy()
  })

  it('renders simple key-value pairs', () => {
    render(<JsonTree data='{"name":"nginx","version":"1.0"}' />)
    expect(screen.getByText('name:')).toBeTruthy()
    expect(screen.getByText(/"nginx"/)).toBeTruthy()
    expect(screen.getByText('version:')).toBeTruthy()
    expect(screen.getByText(/"1.0"/)).toBeTruthy()
  })

  it('renders boolean values', () => {
    render(<JsonTree data='{"admin":true}' />)
    expect(screen.getByText('admin:')).toBeTruthy()
    expect(screen.getByText('true')).toBeTruthy()
  })

  it('renders numeric values', () => {
    render(<JsonTree data='{"quota":1024}' />)
    expect(screen.getByText('quota:')).toBeTruthy()
    expect(screen.getByText('1024')).toBeTruthy()
  })

  it('renders null values', () => {
    render(<JsonTree data='{"field":null}' />)
    expect(screen.getByText('field:')).toBeTruthy()
    expect(screen.getByText('null')).toBeTruthy()
  })

  it('renders nested objects with collapsible tree', () => {
    render(<JsonTree data='{"responses":{"port":"8080"}}' />)
    // The nested object label should be visible
    expect(screen.getByText('responses')).toBeTruthy()
    // The nested value should be visible (expanded by default)
    expect(screen.getByText('port:')).toBeTruthy()
    expect(screen.getByText(/"8080"/)).toBeTruthy()
  })

  it('collapses nested objects when clicked', () => {
    render(<JsonTree data='{"responses":{"port":"8080"}}' />)
    // Click the "responses" node to collapse it
    fireEvent.click(screen.getByText('responses'))
    // After collapsing, the nested key should not be visible
    expect(screen.queryByText('port:')).toBeNull()
    // The count indicator should show
    expect(screen.getByText('{1}')).toBeTruthy()
  })

  it('renders arrays', () => {
    render(<JsonTree data='{"items":["a","b"]}' />)
    expect(screen.getByText('items')).toBeTruthy()
    expect(screen.getByText(/"a"/)).toBeTruthy()
    expect(screen.getByText(/"b"/)).toBeTruthy()
  })

  it('collapses arrays when clicked', () => {
    render(<JsonTree data='{"items":["a","b"]}' />)
    fireEvent.click(screen.getByText('items'))
    expect(screen.queryByText(/"a"/)).toBeNull()
    expect(screen.getByText('[2]')).toBeTruthy()
  })
})
