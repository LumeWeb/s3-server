// S3 backend status: mirrors internal/status/status.go
export enum S3Status {
  Running = 'running',
  Stopped = 'stopped',
  Error = 'error',
}
