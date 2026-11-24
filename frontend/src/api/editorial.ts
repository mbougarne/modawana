export const editorialAPI = '/api/v1/owner/editorial';

export interface EditorialConfig {
  version: number;
  enabled: boolean;
  daily_limit: number;
  configured: boolean;
  key_available: boolean;
  used: number;
}
