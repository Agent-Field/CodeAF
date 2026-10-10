import { useRef, useState, type KeyboardEvent } from 'react';
import { Button, KeyboardShortcut, PageHeading, SectionHeading, Segmented, Text, TextInput } from '../../components/ui';
import type { CatalogModel, ModelRole, PlacesSetting } from '../chat/engine-client';
import { AppearanceSection } from './AppearanceSection';
import { EngineSection } from './EngineSection';
import { KeyStatus } from './KeyStatus';
import { groupRoles, roleStateLine } from './groups';
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
      {roleStateLine(role, settings.roles) && <p className="settings-row-state">{roleStateLine(role, settings.roles)}</p>}
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
 * A whole number typed into a Places setting: saved on Enter or on leaving the field, put back when it is not a whole
 * number or the engine refused it. It is keyed by the saved value, so a reset or a save elsewhere redraws it.
 */
function NumberSetting({ setting, onSave }: { setting: PlacesSetting; onSave: (value: number) => Promise<boolean> }) {
  const saved = String(setting.value);
  const [text, setText] = useState(saved);
  const draftVersion = useRef(0);
  const commit = () => {
    const value = Number(text);
    if (text.trim() === '' || !Number.isInteger(value) || value === setting.value) { setText(saved); return; }
    const version = draftVersion.current;
    void onSave(value).then(ok => { if (!ok && draftVersion.current === version) setText(saved); });
  };
  const keys = (event: KeyboardEvent<HTMLInputElement>) => {
    if (event.key === 'Enter') commit();
    if (event.key === 'Escape') { draftVersion.current++; setText(saved); }
  };
  return (
    <span className="settings-number">
      <TextInput appearance="field" type="number" inputMode="numeric" min={setting.min} max={setting.max} step={1} aria-label={setting.name}
        value={text} onChange={event => { draftVersion.current++; setText(event.target.value); }} onBlur={commit} onKeyDown={keys} />
      {setting.unit && <span className="settings-unit">{setting.unit}</span>}
    </span>
  );
}

function PlacesSettingRow({ setting, settings }: { setting: PlacesSetting; settings: ModelSettings }) {
  return (
    <li className="settings-row" data-setting={setting.key}>
      <div className="settings-row-head">
        <span className="settings-row-name">{setting.name}</span>
        {setting.chosen && <Button variant="ghost" className="settings-reset" aria-label={`Reset ${setting.name}`} onClick={() => settings.savePlaces(setting.key, null)}>Reset</Button>}
      </div>
      <p className="settings-row-controls">{setting.explain}</p>
      {!setting.design && <p className="settings-row-state">Provisional default: {String(setting.default === true ? 'On' : setting.default === false ? 'Off' : setting.default)}{setting.unit && typeof setting.default === 'number' ? ` ${setting.unit}` : ''}, an engineering choice until it is designed.</p>}
      <div className="settings-row-choice">
        {setting.kind === 'switch'
          ? <Segmented label={setting.name} value={setting.value ? 'on' : 'off'} options={[{ value: 'off', label: 'Off' }, { value: 'on', label: 'On' }]} onChange={word => settings.savePlaces(setting.key, word === 'on')} />
          : <NumberSetting key={String(setting.value)} setting={setting} onSave={value => settings.savePlaces(setting.key, value)} />}
      </div>
    </li>
  );
}

/** How codeaf offers places: listed under the Places organization section, after its two model roles. */
function PlacesPolicy({ settings }: { settings: ModelSettings }) {
  if (settings.places.state === 'loading') return null;
  if (settings.places.state === 'unavailable') return <Text className="settings-note">How codeaf offers places cannot be changed from this engine yet.</Text>;
  return (
    <ul className="settings-rows" aria-label="How codeaf offers places">
      {settings.places.settings.map(setting => <PlacesSettingRow key={setting.key} setting={setting} settings={settings} />)}
    </ul>
  );
}

/**
 * The Models page: which three models are pinned to the composer, which model each kind of job runs on, grouped
 * into provisional sections, and how codeaf offers places. Every change is saved at once through the engine and
 * says so; there is no Save button.
 */
export function SettingsPage() {
  const settings = useModelSettings();
  return (
    <div className="settings-page">
      <header className="settings-head">
        <PageHeading>{SETTINGS_TAB_TITLE}</PageHeading>
        <Receipt receipt={settings.receipt} />
      </header>
      <AppearanceSection />
      <EngineSection />
      <ul className="settings-rows" aria-label="Provider key"><KeyStatus /></ul>
      <Text className="settings-note">These settings are provisional. Each job starts on the default model, and the defaults are engineering choices until Settings is designed.</Text>
      {settings.state === 'loading' && <Text>Reading your model choices…</Text>}
      {settings.state === 'unavailable' && <Text role="alert">The engine is not reachable, so model choices cannot be read.</Text>}
      {settings.state === 'ready' && (
        <>
          <PinnedSection settings={settings} />
          {groupRoles(settings.roles, settings.categories).map(({ category, roles }) => {
            const id = `settings-roles-${category.id || 'all'}`;
            return (
              <section key={id} className="models-section" aria-labelledby={id}>
                <SectionHeading id={id}>{category.name}</SectionHeading>
                <ul className="settings-rows">
                  {roles.map(role => <RoleRow key={role.id} role={role} settings={settings} />)}
                </ul>
                {category.id === 'places' && <PlacesPolicy settings={settings} />}
              </section>
            );
          })}
        </>
      )}
    </div>
  );
}
