import type { Locale } from './content';

export type FieldKind = 'text' | 'email' | 'textarea';

export type LeadStatus = 'new' | 'contacted' | 'closed';

export interface FormField {
  key: string;
  label: string;
  kind: FieldKind;
  required: boolean;
}

export interface LeadForm {
  key: string;
  title: string;
  version: number;
  enabled: boolean;
  consent: string;
  retention_days: number;
  fields: FormField[];
}

export interface Lead {
  id: string;
  form_key: string;
  form_version: number;
  locale: Locale;
  page_key: string;
  status: LeadStatus;
  tags: string[];
  consent: string;
  values: Record<string, string>;
  created_at: number;
  notified_at: number;
}

export const formsURL = '/api/v1/owner/forms';

export const leadsURL = '/api/v1/owner/leads';
