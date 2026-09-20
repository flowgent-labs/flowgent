import { Upload } from 'lucide-react'
import { useRef } from 'react'
import { Button } from '../../shared/components/ui'

export function ManifestImportButton({
  label,
  onImport,
}: {
  label: string
  onImport: (source: string, filename: string) => void
}) {
  const input = useRef<HTMLInputElement>(null)
  return (
    <>
      <Button variant="secondary" onClick={() => input.current?.click()}>
        <Upload size={16} />
        {label}
      </Button>
      <input
        ref={input}
        data-testid="manifest-file-input"
        type="file"
        accept=".yaml,.yml,.json,text/yaml,application/json"
        hidden
        onChange={async (event) => {
          const file = event.target.files?.[0]
          event.target.value = ''
          if (!file) return
          onImport(await file.text(), file.name)
        }}
      />
    </>
  )
}
