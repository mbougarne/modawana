/** Wire contracts from docs/api/v1.openapi.json. Domain DTOs arrive with their tickets. */
export const API_ERROR_CODES = [
  'bad_request',
  'unauthenticated',
  'forbidden',
  'not_found',
  'method_not_allowed',
  'revision_conflict',
  'idempotency_conflict',
  'too_large',
  'unsupported_media_type',
  'invalid_fields',
  'rate_limited',
  'internal_error',
  'unavailable',
  'lesson_locked',
] as const;

export type APIErrorCode = (typeof API_ERROR_CODES)[number];

export interface APIErrorEnvelope {
  error: {
    code: APIErrorCode;
    message: string;
    request_id: string;
    fields?: Record<string, string>;
  };
}

export interface RuntimeConfiguration {
  api_version: 'v1';
  default_locale: 'en';
  locales: ('en' | 'ar' | 'fr')[];
}

export interface HealthResponse {
  status: 'ok';
}

/** Owner wire contracts are shared with the feature; authentication API is documented in owner.md. */
export type {
  State as AuthenticationState,
  Session as OwnerSession,
  Key as OwnerPasskey,
  Factors as OwnerFactors,
  Audit as SecurityEvent,
} from '../features/auth/api';

export interface MediaRecord {
  retained_references?: number;
  public_reuse?: boolean;
  id: string;
  url: string;
  kind: 'image' | 'video' | 'pdf' | 'captions';
  mime_type: string;
  byte_size: number;
  filename: string;
  purpose: 'content' | 'logo' | 'favicon' | 'portrait' | 'banner';
  width: number;
  height: number;
  created_at: string;
}

export interface MediaPage {
  items: MediaRecord[];
  next_cursor: string;
}

export type * from './content';
