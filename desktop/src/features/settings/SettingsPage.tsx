import { Button, KeyboardShortcut, PageHeading, SectionHeading, Segmented, Text } from '../../components/ui';
import type { CatalogModel, ModelRole } from '../chat/engine-client';
import { ModelSelect } from './ModelSelect';
import { effortWord, SETTINGS_TAB_TITLE } from './summary';
import { useModelSettings, type ModelSettings } from './useModelSettings';
import './settings.css';

const EFFORT_ORDER = ['low', 'medium', 'high'];

/** The effort words the chosen model accepts, in reading order; empty when it takes none. */
const effortsOf = (catalog: readonly CatalogModel[], model: string) =>
  EFFORT_ORDER.filter(word => catalog.find(row => row.id === model)?.efforts?.includes(word));

function Receipt({ receipt }: { receipt: ModelSettings['receipt'] }) {
  return <p className="settings-receipt" role="status" data-failed={receipt?.failed || undefined}>{receipt?.text ?? ''}</p>;
}

function PinnedSection({ settings }: { settings: ModelSettings }) {
  return (
    <section className="models-section" aria-labelledby="settings-pinned">
      <SectionHeading id="settings-pinned">Pinned</SectionHeading>
      <Text className="settings-note">The composer's model control shows these three, in this order.</Text>
      <ul className="settings-rows">
        {settings.pinned.map((model, slot) => (
          <li key={slot} className="settings-row settings-row-inline">
            <div className="settings-row-text">
              <span className="settings-row-name">{model.label}</span>
              <KeyboardShortcut command={String(slot + 1)} />
            </div>
            <ModelSelect label={`Pinned model ${slot + 1}`} value={model.id} catalog={settings.catalog} onChange={id => settings.savePin(slot, id)} />
          </li>
        ))}
      </ul>
      {settings.pinsChosen && <Button variant="ghost" className="settings-reset" onClick={settings.resetPins}>Reset pinned models</Button>}
    </section>
  );
}

function RoleRow({ role, settings }: { role: ModelRole; settings: ModelSettings }) {
  const efforts = effortsOf(settings.catalog, role.model);
  const choose = (model: string) => settings.saveRole(role.id, { model });
  return (
    <li className="settings-row" data-role={role.id}>
      <div className="settings-row-head">
        <span className="settings-row-name">{role.name}</span>
        {role.chosen && <Button variant="ghost" className="settings-reset" aria-label={`Reset ${role.name}`} onClick={() => settings.saveRole(role.id, { model: '' })}>Reset</Button>}
      </div>
      <p className="settings-row-controls">{role.controls}</p>
      <div className="settings-row-choice">
        <ModelSelect label={`Model for ${role.name}`} value={role.model} catalog={settings.catalog} onChange={choose} />
        {efforts.length > 0 && (
          <Segmented label={`Effort for ${role.name}`} value={role.effort ?? ''} options={efforts.map(word => ({ value: word, label: effortWord(word) }))} onChange={word => settings.saveRole(role.id, { model: role.model, effort: word })} />
        )}
      </div>
    </li>
  );
}

/**
 * The Models page: which three models are pinned to the composer, and which model each kind of job runs on.
 * Every change is saved at once through the engine and says so; there is no Save button.
 */
export function SettingsPage() {
  const settings = useModelSettings();
  return (
    <div className="settings-page">
      <header className="settings-head">
        <PageHeading>{SETTINGS_TAB_TITLE}</PageHeading>
        <Receipt receipt={settings.receipt} />
      </header>
      {settings.state === 'loading' && <Text>Reading your model choices…</Text>}
      {settings.state === 'unavailable' && <Text role="alert">The engine is not reachable, so model choices cannot be read.</Text>}
      {settings.state === 'ready' && (
        <>
          <PinnedSection settings={settings} />
          <section className="models-section" aria-labelledby="settings-roles">
            <SectionHeading id="settings-roles">Jobs</SectionHeading>
            <Text className="settings-note">Each kind of job runs on one model. Every job starts on the default.</Text>
            <ul className="settings-rows">
              {settings.roles.map(role => <RoleRow key={role.id} role={role} settings={settings} />)}
            </ul>
          </section>
        </>
      )}
    </div>
  );
}
