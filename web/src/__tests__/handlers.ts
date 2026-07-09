// MSW request handlers — intercepts HTTP requests during tests.
// Each handler mocks a Panel API endpoint, matching the paths used by api.ts.

import { http, HttpResponse } from 'msw'

// Default handlers — tests can override these with server.use()
export const handlers = [
  // Status endpoint — returns "running" by default
  http.get('/_panel/api/status', () =>
    HttpResponse.json({
      s3_status: 'running',
      init_error: '',
      key_count: 3,
      version: '1.0.0-test',
    }),
  ),

  // Version / update check
  http.get('/_panel/api/version', () =>
    HttpResponse.json({
      update_available: false,
      latest_version: '1.0.0-test',
    }),
  ),

  // Admin stats (monitoring page)
  http.get('/_panel/api/admin/stats', () =>
    HttpResponse.json({
      pending_objects: 0,
      pending_size: 0,
      uploaded_objects: 42,
      uploaded_size: 1024 * 1024,
      failed_uploads: 0,
      orphaned_objects: 0,
      multipart_uploads: 0,
      unpinned_objects: 0,
    }),
  ),

  // Buckets list
  http.get('/_panel/api/buckets', () =>
    HttpResponse.json([
      { name: 'test-bucket', creation_date: '2024-01-01T00:00:00Z' },
    ]),
  ),

  // Onboarding status
  http.get('/_panel/api/onboarding/status', () =>
    HttpResponse.json({ state: 'pending' }),
  ),

  // Onboarding config
  http.get('/_panel/api/onboarding/config', () =>
    HttpResponse.json({
      app_id: 'test-app-id',
      app_name: 'Test App',
      app_description: 'Test description',
      logo_url: '',
      service_url: 'https://test.example.com',
      callback_url: 'https://test.example.com/callback',
      indexer_url: 'https://indexer.example.com',
      available_indexers: ['https://indexer.example.com'],
    }),
  ),

  // S3 config
  http.get('/_panel/api/s3-config', () =>
    HttpResponse.json({
      directory: '/data/s3',
      indexer_url: 'https://indexer.example.com',
      available_indexers: ['https://indexer.example.com'],
      host_bases: ['s3.test.example.com'],
    }),
  ),

  // SSL config
  http.get('/_panel/api/ssl-config', () =>
    HttpResponse.json({
      mode: 'none',
      acme_email: '',
      acme_dir_url: '',
    }),
  ),
]
