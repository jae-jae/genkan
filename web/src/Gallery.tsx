import { useEffect, useState } from 'preact/hooks'
import { Icon } from './icons'
import { t } from './i18n'
import { gallery, loadGallery, showOver, uploadWallpapers, uploading, version, deleteWallpaper } from './store'
import { PRESETS } from './util'

/**
 * Wallpaper chooser: the 6 presets plus "my gallery" (uploaded images). `manage` adds the delete buttons
 * (only valid inside Settings, where a confirm can be stacked on top).
 */
export function WallpaperGallery({ value, onPick, manage }: { value: string; onPick: (v: string) => void; manage?: boolean }) {
  const [hot, setHot] = useState(false)
  const files = gallery.value
  const cur = value || 'preset:aurora'
  const up = uploading.value
  useEffect(() => void loadGallery(), [version.value])

  const upload = async (list: FileList | File[] | null | undefined) => {
    const fs = Array.from(list || []).filter((f) => f.type.startsWith('image/') || !f.type)
    if (!fs.length || uploading.value) return
    const vals = await uploadWallpapers(fs)
    if (fs.length === 1 && vals.length === 1) onPick(vals[0])
  }
  const askDelete = (v: string) =>
    showOver({
      kind: 'confirm',
      title: t('wp.deleteTitle'),
      message: t('wp.deleteMsg'),
      confirmLabel: t('common.delete'),
      onConfirm: () => void deleteWallpaper(v),
    })

  return (
    <div class="gal" data-testid="wp-picker">
      <div class="gal-grid" data-testid="wp-presets">
        {PRESETS.map((id) => (
          <button type="button" key={id} class={'wp-thumb wp-' + id} aria-pressed={cur === 'preset:' + id} aria-label={t('wp.' + id)} title={t('wp.' + id)} data-testid={'wp-preset-' + id} onClick={() => onPick('preset:' + id)}>
            <span>{t('wp.' + id)}</span>
          </button>
        ))}
      </div>
      <h4 class="gal-h">
        {t('wp.mine')}
        {files && files.length > 0 && <small>{files.length}</small>}
      </h4>
      <div
        class={'gal-grid gal-mine' + (hot ? ' hot' : '')}
        data-testid="wp-gallery"
        onDragOver={(e) => {
          if (e.dataTransfer?.types.includes('Files')) {
            e.preventDefault()
            setHot(true)
          }
        }}
        onDragLeave={(e) => e.currentTarget === e.target && setHot(false)}
        onDrop={(e) => {
          e.preventDefault()
          setHot(false)
          upload(e.dataTransfer?.files)
        }}
      >
        {(files || []).map((f) => {
          const name = f.value.replace(/^upload:/, '')
          return (
            <div class="wp-item" key={f.value}>
              <button type="button" class="wp-thumb wp-up" aria-pressed={cur === f.value} aria-label={t('wp.custom') + ' ' + name} data-testid={'gallery-item-' + name} data-value={f.value} onClick={() => onPick(f.value)}>
                <img src={f.url} alt="" loading="lazy" decoding="async" draggable={false} />
              </button>
              {manage && (
                <button type="button" class="wp-del" aria-label={t('wp.delete')} title={t('wp.delete')} data-testid={'gallery-delete-' + name} onClick={() => askDelete(f.value)}>
                  <Icon n="x" size={13} />
                </button>
              )}
            </div>
          )
        })}
        <label class="wp-thumb wp-upload" data-testid="wp-upload" aria-busy={!!up} tabIndex={0} onKeyDown={(e) => (e.key === 'Enter' || e.key === ' ') && (e.preventDefault(), (e.currentTarget.querySelector('input') as HTMLInputElement).click())}>
          {up ? (
            <>
              <span class="spin" aria-hidden="true" />
              <span data-testid="wp-upload-progress">
                {up.done}/{up.total}
              </span>
            </>
          ) : (
            <>
              <Icon n="upload" size={20} />
              <span>{t('wp.upload')}</span>
            </>
          )}
          <input
            type="file"
            hidden
            multiple
            accept="image/jpeg,image/png,image/webp,image/avif"
            data-testid="wp-upload-input"
            onChange={(e) => {
              const el = e.target as HTMLInputElement
              upload(el.files).then(() => (el.value = ''))
            }}
          />
        </label>
      </div>
      {files && !files.length && !up && <p class="hint gal-empty">{t('wp.empty')}</p>}
    </div>
  )
}
