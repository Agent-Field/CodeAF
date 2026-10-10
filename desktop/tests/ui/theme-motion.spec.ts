import { openAppearance, openPage } from './support/shell-navigation';
import design from '../../src/design/tokens.json' with { type: 'json' };
import { test, expect, type Page } from '@playwright/test';
import { expectAccessible, expectNoUnstyledControls, expectThemedSurface, tokenColor, tokenColorIn } from './contracts';
async function chooseTheme(page: Page, label: string) {
 await openAppearance(page);
 await page.getByRole('option',{name:label,exact:true}).click();
 await expect(page.getByRole('listbox')).not.toBeVisible();
 await expect(page.locator('#root')).not.toHaveAttribute('aria-hidden','true');
}
for (const theme of ['Light','Dark','System']) {
 test(`${theme}: themed menus, palette, keyboard focus and accessible controls`, async ({page})=>{
  await page.emulateMedia({colorScheme:'light'});
  await page.goto('/');
  await chooseTheme(page,`${theme} appearance`);
  await expect(page.locator('html')).toHaveAttribute('data-resolved-theme',theme==='Dark'?'dark':'light');
  await expectAccessible(page);
  await expectNoUnstyledControls(page);
  const trigger = page.getByRole('combobox',{name:'Theme'});
  await trigger.focus(); await page.keyboard.press('Enter');
  const menu = page.getByRole('listbox'); await expect(menu).toBeVisible();
  await expectThemedSurface(page,menu);
  await expect(page.locator('#root')).toHaveAttribute('inert','');
  const highlighted = page.locator('.select-option[data-highlighted]');
  await expect(highlighted).toHaveCSS('background-color',await tokenColor(page,'menu-highlight'));
  await expectAccessible(page);
  await page.keyboard.press('Escape');
  await expect(trigger).toBeFocused();
  await expect(trigger).toHaveCSS('outline-width',design.foundation['focus-ring-width']);
  // ⌘K is the New-tab field (SH-OQ5): focused, no dialog, themed like the rest of the card.
  await page.keyboard.press('Control+k');
  const field=page.getByRole('combobox',{name:'Search or start'});
  await expect(field).toBeFocused(); await expect(page.getByRole('dialog')).toHaveCount(0);
  await expectAccessible(page);
  await openPage(page, 'Design system');
  await expectAccessible(page);
  await expectNoUnstyledControls(page);
  await page.setViewportSize({width:800,height:560});
  expect(await page.evaluate(()=>document.documentElement.scrollWidth<=innerWidth)).toBe(true);
  if(theme==='System') {
   await page.emulateMedia({colorScheme:'dark'});
   await expect(page.locator('html')).toHaveAttribute('data-resolved-theme','dark');
   await openAppearance(page); await expectThemedSurface(page,page.getByRole('listbox'));
  }
 });
}
test('navigation is still; action motion is bounded; collapse has a real transition', async ({page})=>{
 await page.goto('/');
 const navigation=page.getByRole('button',{name:'Inbox',exact:true});
 await navigation.hover();
 await expect(navigation.locator('.app-icon')).toHaveAttribute('data-motion','none');
 expect(await navigation.locator('svg').evaluate(el=>el.getAnimations({subtree:true}).length)).toBe(0);
 await openPage(page, 'Design system');
 const action=page.getByRole('button',{name:'Open the new-tab field',exact:true});
 await action.hover();
 await expect(action.locator('.app-icon')).toHaveCSS('transform',`matrix(1, 0, 0, 1, ${parseFloat(design.foundation['motion-directional-travel'])}, 0)`);
 await expect(action.locator('.app-icon')).toHaveCSS('transition-duration',`${parseFloat(design.foundation['duration-directional'])/1000}s`);
 await page.getByRole('button',{name:'Hide sidebar'}).click();
 const shell=page.locator('.app-shell');
 await expect(shell).toHaveCSS('transition-duration',Array(2).fill(`${parseFloat(design.foundation['duration-sidebar'])/1000}s`).join(', '));
 await expect.poll(async()=>page.locator('.content-pane').evaluate(el=>Math.round(el.getBoundingClientRect().left))).toBe(parseFloat(design.foundation['shell-card-inset']));
 await expect(navigation).not.toBeVisible();
 await page.getByRole('button',{name:'Show sidebar'}).click();
 await expect.poll(async()=>page.locator('.content-pane').evaluate(el=>Math.round(el.getBoundingClientRect().left))).toBe(parseFloat(design.foundation['sidebar-width']));
});
test('reduced motion disables every shared transition and keyframe',async({page})=>{
 await page.emulateMedia({reducedMotion:'reduce'}); await page.goto('/');
 await expect(page.locator('.app-shell')).toHaveCSS('transition-duration','0s, 0s');
 await openPage(page, 'Design system');
 await page.getByRole('button',{name:'Open the new-tab field',exact:true}).hover();
 await expect(page.getByRole('button',{name:'Open the new-tab field',exact:true}).locator('.app-icon')).toHaveAttribute('data-motion','none');
 await openAppearance(page);
 await expect(page.getByRole('listbox')).toHaveCSS('animation-duration','0s');
 await page.keyboard.press('Escape'); await page.keyboard.press('Control+k');
 await expect(page.getByRole('combobox',{name:'Search or start'})).toBeFocused();
});
test('theme menu keyboard selection, persistence, outside dismissal and selection state',async({page})=>{
 await page.goto('/'); await openPage(page, 'Settings'); const trigger=page.getByRole('combobox',{name:'Theme'});
 await trigger.focus(); await page.keyboard.press('Enter');
 await expect(page.getByRole('option',{name:'System appearance'})).toBeFocused();
 await page.keyboard.press('End');
 await expect(page.getByRole('option',{name:'Dark appearance'})).toBeFocused();
 await page.keyboard.press('Enter');
 await expect(page.locator('html')).toHaveAttribute('data-theme','dark');
 await page.reload(); await expect(page.locator('html')).toHaveAttribute('data-theme','dark');
 const outside = await page.locator('.content-pane').boundingBox();
 await trigger.click(); await expect(page.getByRole('option',{name:'Dark appearance'})).toHaveAttribute('aria-selected','true');
 await expect(page.getByRole('option',{name:'Dark appearance'})).toBeFocused();
 await expectThemedSurface(page, page.getByRole('listbox'));
 await page.mouse.click(outside!.x+outside!.width/2,outside!.y+outside!.height/2);
 await expect(page.getByRole('listbox')).not.toBeVisible();
 await openPage(page, 'Activity');
 await expect(page.getByRole('heading',{name:'Activity',exact:true})).toBeVisible();
 await expect(page.getByRole('button',{name:'Now',exact:true})).not.toHaveAttribute('aria-current','page');
});

for (const theme of ['Light','Dark']) {
 test(`${theme}: shared hover, press, selection and disabled states`,async({page,browserName})=>{
  await page.goto('/'); await chooseTheme(page,`${theme} appearance`);
  const quick=page.getByRole('button',{name:'Inbox',exact:true});
  await expect(quick).not.toHaveAttribute('aria-current','page');
  await quick.hover(); await expect(quick).toHaveCSS('background-color',await tokenColor(page,'tab-hover'));
  await page.mouse.down(); await page.mouse.up();
  await page.mouse.move(700,30);
  await expect(quick).toHaveAttribute('aria-current','page');
  await expect(quick).toHaveCSS('background-color',await tokenColor(page,'tab'));
  await expect(page.getByRole('button',{name:'Now',exact:true}).first()).not.toHaveAttribute('aria-current','page');
  await expect(page.getByRole('button',{name:'codeaf',exact:true})).toHaveCount(0);
  await openPage(page, 'Design system');
  // Design v3 controls: hover changes only the fill, press darkens it, focus is the accent ring and halo.
  const controls=page.locator('.controls-specimen');
  const quiet=controls.getByRole('button',{name:'Deny',exact:true});
  await expect(quiet).toHaveCSS('background-color',await tokenColorIn(controls,'field'));
  await quiet.hover(); await expect(quiet).toHaveCSS('background-color',await tokenColorIn(controls,'field-2'));
  await page.mouse.down(); await expect(quiet).toHaveCSS('filter',`brightness(${Number(design.foundation['brightness-press'])})`); await page.mouse.up();
  // macOS WebKit uses Option-Tab to include buttons in keyboard navigation.
  await quiet.focus(); await page.keyboard.press(browserName==='webkit'?'Alt+Tab':'Tab'); await page.keyboard.press(browserName==='webkit'?'Alt+Shift+Tab':'Shift+Tab');
  await expect(quiet).toBeFocused();
  expect(await quiet.evaluate(node=>getComputedStyle(node).boxShadow)).toContain(await tokenColorIn(controls,'accent'));
  await expect(controls.getByRole('button',{name:'Disabled',exact:true})).toHaveCSS('opacity',design.foundation['opacity-control-disabled']);
  const row=controls.locator('.row').nth(1);
  await expect(row.locator('.row-actions')).toHaveCSS('opacity','0');
  await row.hover(); await expect(row.locator('.row-actions')).toHaveCSS('opacity','1');
  const copy=controls.getByRole('button',{name:'Copy',exact:true});
  await copy.hover(); await expect(page.getByRole('tooltip',{name:'Copy'})).toBeVisible();
  await expect(controls.getByRole('button',{name:'Disabled',exact:true})).toBeDisabled();
 });
}

test('settings appearance: theme applies at once, survives a reload, and reduce motion has no control', async ({ page }) => {
 await page.goto('/'); await openPage(page, 'Settings');
 const section = page.getByRole('region', { name: 'Appearance' });
 await expect(section.getByText('Reduce motion follows your system setting.')).toBeVisible();
 await expect(section.getByRole('combobox')).toHaveCount(1);
 await chooseTheme(page, 'Dark appearance');
 await expect(page.locator('html')).toHaveAttribute('data-theme', 'dark');
 await page.reload();
 await expect(page.locator('html')).toHaveAttribute('data-theme', 'dark');
});
