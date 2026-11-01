export function portraitCrop(width: number, height: number) {
  const side = Math.min(width, height);
  if (side < 600 || width * height > 24000000) {
    throw new Error('validation.portrait_media');
  }
  return { x: (width - side) / 2, y: (height - side) / 2, side, size: Math.min(side, 1200) };
}

/** Prepare the square portrait required by the server; never change the source file. */
export async function preparePortrait(file: File): Promise<File> {
  if (!['image/jpeg', 'image/png', 'image/webp'].includes(file.type) || file.size > 10 * 1048576) {
    throw new Error('validation.portrait_media');
  }
  const bitmap = await createImageBitmap(file);
  try {
    const crop = portraitCrop(bitmap.width, bitmap.height);
    const canvas = document.createElement('canvas');
    canvas.width = canvas.height = crop.size;
    const context = canvas.getContext('2d');
    if (!context) {
      throw new Error('config.invalidUpload');
    }
    context.drawImage(bitmap, crop.x, crop.y, crop.side, crop.side, 0, 0, crop.size, crop.size);
    const blob = await new Promise<Blob>((resolve, reject) =>
      canvas.toBlob(
        (value) => (value ? resolve(value) : reject(new Error('config.invalidUpload'))),
        'image/webp',
        0.9,
      ),
    );
    if (blob.size > 2 * 1048576) {
      throw new Error('validation.portrait_media');
    }
    return new File([blob], file.name.replace(/\.[^.]+$/, '') + '-portrait.webp', {
      type: 'image/webp',
    });
  } finally {
    bitmap.close();
  }
}
