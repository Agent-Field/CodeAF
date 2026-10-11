import { useQueue } from "@/api/queue"
import { useSettings } from "@/api/settings"
import { FactorySettings } from "@/components/settings-factory"
import { YouSettings } from "@/components/settings-you"
import { Alert, AlertDescription } from "@/components/ui/alert"
import { onLoopback } from "@/lib/settings"

// Settings (DESIGN-BRIEF §4.5): this factory, then you. The address under
// the title is the one the factory reports, which is also the attach command.
export function SettingsPage() {
  const { queue } = useQueue()
  const { github, repos, me, error, connect, watch } = useSettings()
  const address = github?.factory.address || me?.factory.address || queue?.factory.address || ""
  const loopback = onLoopback(window.location.hostname)

  return (
    <div className="mx-auto flex max-w-3xl flex-col gap-8" data-testid="settings-page">
      <div>
        <h1 className="text-xl font-semibold">Settings</h1>
        <p className="text-sm text-muted-foreground" data-testid="settings-address">
          {address}
        </p>
      </div>
      {error && (
        <Alert variant="destructive" data-testid="settings-error">
          <AlertDescription>{error}</AlertDescription>
        </Alert>
      )}
      <section className="flex flex-col gap-3">
        <h2 className="text-lg font-semibold">This factory</h2>
        <FactorySettings github={github} repos={repos} loopback={loopback} onConnect={connect} onWatch={watch} />
      </section>
      <section className="flex flex-col gap-3">
        <h2 className="text-lg font-semibold">You</h2>
        <YouSettings me={me} repos={repos} address={address} loopback={loopback} />
      </section>
    </div>
  )
}
