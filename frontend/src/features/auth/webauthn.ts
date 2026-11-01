// Native browser ceremonies retain provider/phone/security-key choice.
function bytes(value: string): ArrayBuffer {
  const binary = atob(value.replace(/-/g, '+').replace(/_/g, '/'));
  return Uint8Array.from(binary, (c) => c.charCodeAt(0)).buffer;
}
function encoded(value: ArrayBuffer | null): string | null {
  if (value === null) {
    return null;
  }
  return btoa(String.fromCharCode(...new Uint8Array(value)))
    .replace(/\+/g, '-')
    .replace(/\//g, '_')
    .replace(/=+$/, '');
}
type DescriptorJSON = {
  id: string;
  type: PublicKeyCredentialType;
  transports?: AuthenticatorTransport[];
};

export type CreationJSON = Omit<
  PublicKeyCredentialCreationOptions,
  'challenge' | 'user' | 'excludeCredentials'
> & {
  challenge: string;
  user: Omit<PublicKeyCredentialUserEntity, 'id'> & { id: string };
  excludeCredentials?: DescriptorJSON[];
};

export type RequestJSON = Omit<
  PublicKeyCredentialRequestOptions,
  'challenge' | 'allowCredentials'
> & { challenge: string; allowCredentials?: DescriptorJSON[] };

export function creationOptions(p: CreationJSON): PublicKeyCredentialCreationOptions {
  return {
    ...p,
    challenge: bytes(p.challenge),
    user: { ...p.user, id: bytes(p.user.id) },
    excludeCredentials: p.excludeCredentials?.map((c) => ({ ...c, id: bytes(c.id) })),
  };
}

export function requestOptions(p: RequestJSON): PublicKeyCredentialRequestOptions {
  return {
    ...p,
    challenge: bytes(p.challenge),
    allowCredentials: p.allowCredentials?.map((c) => ({ ...c, id: bytes(c.id) })),
  };
}

export function credentialJSON(credential: PublicKeyCredential) {
  const r = credential.response;
  const response =
    r instanceof AuthenticatorAttestationResponse
      ? {
          clientDataJSON: encoded(r.clientDataJSON),
          attestationObject: encoded(r.attestationObject),
          transports: r.getTransports?.() || [],
        }
      : {
          clientDataJSON: encoded(r.clientDataJSON),
          authenticatorData: encoded((r as AuthenticatorAssertionResponse).authenticatorData),
          signature: encoded((r as AuthenticatorAssertionResponse).signature),
          userHandle: encoded((r as AuthenticatorAssertionResponse).userHandle),
        };
  return {
    id: credential.id,
    rawId: encoded(credential.rawId),
    type: credential.type,
    response,
    clientExtensionResults: credential.getClientExtensionResults(),
    authenticatorAttachment: credential.authenticatorAttachment,
  };
}
