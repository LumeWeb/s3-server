import { describe, it, expect } from 'vitest'
import {
  validatePasswordMatch,
  buildS3ConfigPayload,
  readJSONScript,
  fmtBytes,
  accountPct,
  nextVersioningStatus,
  bucketLifecycleURL,
  bucketVersioningURL,
  normalizeLifecycleRules,
  MIN_PASSWORD_LENGTH,
} from '../src/utils'

// --- Password validation ---

describe('validatePasswordMatch', () => {
  it('returns null when passwords match and meet minimum length', () => {
    expect(validatePasswordMatch('password123', 'password123')).toBeNull()
  })

  it('returns error when passwords do not match', () => {
    expect(validatePasswordMatch('password123', 'password456')).toBe('Passwords do not match')
  })

  it('returns error when password is too short', () => {
    expect(validatePasswordMatch('short', 'short')).toBe(
      `Password must be at least ${MIN_PASSWORD_LENGTH} characters`,
    )
  })

  it('returns error for empty strings', () => {
    expect(validatePasswordMatch('', '')).toBe(
      `Password must be at least ${MIN_PASSWORD_LENGTH} characters`,
    )
  })

  it('checks mismatch before length (mismatch takes priority)', () => {
    expect(validatePasswordMatch('short', 'other')).toBe('Passwords do not match')
  })

  it('accepts exactly minimum length', () => {
    const pw = 'a'.repeat(MIN_PASSWORD_LENGTH)
    expect(validatePasswordMatch(pw, pw)).toBeNull()
  })
})

// --- S3 config payload builder ---

describe('buildS3ConfigPayload', () => {
  const baseInput = {
    directory: '/data',
    indexer_selection: 'https://sia.lobstr.co',
    custom_indexer: '',
    host_bases_input: '',
    disk_usage_limit: '100',
    upload_waste_pct: 0.1,
  }

  it('builds payload with selected indexer', () => {
    const payload = buildS3ConfigPayload({ ...baseInput, host_bases_input: 'a.example.com, b.example.com' })
    expect(payload.directory).toBe('/data')
    expect(payload.indexer_url).toBe('https://sia.lobstr.co')
    expect(payload.host_bases).toEqual(['a.example.com', 'b.example.com'])
    expect(payload.disk_usage_limit).toBe('100')
    expect(payload.upload_waste_pct).toBe(0.1)
  })

  it('uses custom indexer when selection is __custom__', () => {
    const payload = buildS3ConfigPayload({
      ...baseInput,
      indexer_selection: '__custom__',
      custom_indexer: 'https://my.indexer.com',
    })
    expect(payload.indexer_url).toBe('https://my.indexer.com')
  })

  it('trims and filters empty host bases', () => {
    const payload = buildS3ConfigPayload({
      ...baseInput,
      host_bases_input: '  a.com  , , b.com  ,  ',
    })
    expect(payload.host_bases).toEqual(['a.com', 'b.com'])
  })

  it('returns empty host bases array for empty input', () => {
    const payload = buildS3ConfigPayload({ ...baseInput, host_bases_input: '' })
    expect(payload.host_bases).toEqual([])
  })

  it('returns empty host bases array for whitespace-only input', () => {
    const payload = buildS3ConfigPayload({ ...baseInput, host_bases_input: '   ,   ,   ' })
    expect(payload.host_bases).toEqual([])
  })
})

// --- readJSONScript ---

describe('readJSONScript', () => {
  it('returns parsed JSON from a script element', () => {
    document.body.innerHTML = '<script type="application/json" id="test-data">{"foo":"bar"}</script>'
    const result = readJSONScript('test-data', null)
    expect(result).toEqual({ foo: 'bar' })
  })

  it('returns fallback when element does not exist', () => {
    document.body.innerHTML = ''
    const result = readJSONScript('missing', { default: true })
    expect(result).toEqual({ default: true })
  })

  it('returns fallback when element has no text content', () => {
    document.body.innerHTML = '<script type="application/json" id="empty"></script>'
    const result = readJSONScript('empty', 42)
    expect(result).toBe(42)
  })

  it('parses arrays', () => {
    document.body.innerHTML = '<script type="application/json" id="arr">[1,2,3]</script>'
    const result = readJSONScript<number[]>('arr', [])
    expect(result).toEqual([1, 2, 3])
  })
})

// --- fmtBytes ---

describe('fmtBytes', () => {
  it('returns "0 B" for zero', () => {
    expect(fmtBytes(0)).toBe('0 B')
  })

  it('returns "0 B" for undefined', () => {
    expect(fmtBytes(undefined as unknown as number)).toBe('0 B')
  })

  it('formats bytes', () => {
    expect(fmtBytes(500)).toBe('500 B')
  })

  it('formats kilobytes', () => {
    expect(fmtBytes(1500)).toBe('1.5 KB')
  })

  it('formats megabytes', () => {
    expect(fmtBytes(1500000)).toBe('1.5 MB')
  })

  it('formats gigabytes', () => {
    expect(fmtBytes(1500000000)).toBe('1.5 GB')
  })

  it('formats terabytes', () => {
    expect(fmtBytes(1500000000000)).toBe('1.5 TB')
  })

  it('rounds to 2 decimal places', () => {
    expect(fmtBytes(1024)).toBe('1.02 KB')
  })

  it('caps at exabytes (largest unit)', () => {
    const huge = 1e24 // larger than EB
    const result = fmtBytes(huge)
    expect(result).toContain('EB')
  })
})

// --- accountPct ---

describe('accountPct', () => {
  it('returns 0 when max is 0', () => {
    expect(accountPct(100, 0)).toBe(0)
  })

  it('returns 0 when max is undefined', () => {
    expect(accountPct(100, undefined as unknown as number)).toBe(0)
  })

  it('calculates percentage', () => {
    expect(accountPct(250, 1000)).toBe(25)
  })

  it('returns 100 when pinned equals max', () => {
    expect(accountPct(1000, 1000)).toBe(100)
  })

  it('handles values over 100%', () => {
    expect(accountPct(1500, 1000)).toBe(150)
  })
})

// --- nextVersioningStatus ---

describe('nextVersioningStatus', () => {
  it('returns Suspended when current is Enabled', () => {
    expect(nextVersioningStatus('Enabled')).toBe('Suspended')
  })

  it('returns Enabled when current is Suspended', () => {
    expect(nextVersioningStatus('Suspended')).toBe('Enabled')
  })

  it('returns Enabled for any non-Enabled status', () => {
    expect(nextVersioningStatus('')).toBe('Enabled')
    expect(nextVersioningStatus('Disabled')).toBe('Enabled')
    expect(nextVersioningStatus('undefined')).toBe('Enabled')
  })

  it('toggles back and forth', () => {
    let status = 'Enabled'
    status = nextVersioningStatus(status)
    expect(status).toBe('Suspended')
    status = nextVersioningStatus(status)
    expect(status).toBe('Enabled')
  })
})

// --- Bucket URL builders ---

describe('bucketLifecycleURL', () => {
  it('builds lifecycle URL with encoded bucket name', () => {
    expect(bucketLifecycleURL('my-bucket', 'get')).toBe('/_panel/api/buckets/my-bucket/lifecycle')
  })

  it('encodes special characters in bucket name', () => {
    expect(bucketLifecycleURL('my bucket', 'get')).toBe('/_panel/api/buckets/my%20bucket/lifecycle')
  })

  it('is the same URL regardless of action parameter', () => {
    // The action param is for callers to express intent; URL is identical
    const getURL = bucketLifecycleURL('test', 'get')
    const putURL = bucketLifecycleURL('test', 'put')
    const delURL = bucketLifecycleURL('test', 'delete')
    expect(getURL).toBe(putURL)
    expect(putURL).toBe(delURL)
  })
})

describe('bucketVersioningURL', () => {
  it('builds versioning URL', () => {
    expect(bucketVersioningURL('my-bucket')).toBe('/_panel/api/buckets/my-bucket/versioning')
  })

  it('encodes special characters', () => {
    expect(bucketVersioningURL('buck&ets')).toBe('/_panel/api/buckets/buck%26ets/versioning')
  })
})

// --- normalizeLifecycleRules ---

describe('normalizeLifecycleRules', () => {
  it('returns empty array for null input', () => {
    expect(normalizeLifecycleRules(null as unknown as any[])).toEqual([])
  })

  it('returns empty array for undefined input', () => {
    expect(normalizeLifecycleRules(undefined as unknown as any[])).toEqual([])
  })

  it('returns empty array for empty array input', () => {
    expect(normalizeLifecycleRules([])).toEqual([])
  })

  it('normalizes a complete rule', () => {
    const rules = [{ prefix: 'logs/', expiration_days: 90, status: 'Enabled' }]
    expect(normalizeLifecycleRules(rules)).toEqual([
      { prefix: 'logs/', expiration_days: 90, status: 'Enabled' },
    ])
  })

  it('applies defaults for missing fields', () => {
    const rules = [{ prefix: 'temp/' }]
    expect(normalizeLifecycleRules(rules)).toEqual([
      { prefix: 'temp/', expiration_days: 30, status: 'Enabled' },
    ])
  })

  it('defaults empty prefix to empty string', () => {
    const rules = [{ expiration_days: 7 }]
    expect(normalizeLifecycleRules(rules)).toEqual([
      { prefix: '', expiration_days: 7, status: 'Enabled' },
    ])
  })

  it('normalizes multiple rules', () => {
    const rules = [
      { prefix: 'a/', expiration_days: 1, status: 'Enabled' },
      { prefix: 'b/', expiration_days: 30, status: 'Disabled' },
      { prefix: '', expiration_days: 365, status: 'Enabled' },
    ]
    const result = normalizeLifecycleRules(rules)
    expect(result).toHaveLength(3)
    expect(result[1]).toEqual({ prefix: 'b/', expiration_days: 30, status: 'Disabled' })
  })
})
