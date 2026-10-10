import {test,expect} from '@playwright/test';
import tauri from '../../src-tauri/tauri.conf.json' with {type:'json'};

// Browser enforcement of the packaged application's real CSP, rather than
// a string assertion: locally created attachment previews must decode.
test('packaged CSP permits a local PNG attachment while refusing a remote image',async({page})=>{
 await page.route(new URL('/',test.info().project.use.baseURL as string).href,async route=>{
  await route.fulfill({contentType:'text/html',body:'<!doctype html><html><body>Local image policy fixture</body></html>',headers:{'content-security-policy':tauri.app.security.csp}});
 });
 await page.goto('/');
 const decoded=await page.evaluate(async()=>{
  const violations:string[]=[];document.addEventListener('securitypolicyviolation',event=>violations.push(event.blockedURI));
  const bytes=Uint8Array.from(atob('iVBORw0KGgoAAAANSUhEUgAAABQAAAAKCAIAAAA7N+mxAAAAF0lEQVR4nGNUWd7JQC5gIlvnqOYRoxkAQX4BaBYmW8oAAAAASUVORK5CYII='),character=>character.charCodeAt(0));
  const url=URL.createObjectURL(new Blob([bytes],{type:'image/png'}));
  const image=new Image();image.src=url;document.body.append(image);
  let decoded=false;try{await image.decode();decoded=true}catch{/* a blocked blob fails here */}
  const remote=new Image();remote.src='https://untrusted.invalid/preview.png';document.body.append(remote);
  await new Promise<void>(resolve=>{remote.onload=()=>resolve();remote.onerror=()=>resolve()});
  const result={decoded,width:image.naturalWidth,height:image.naturalHeight,violations};URL.revokeObjectURL(url);image.remove();remote.remove();return result;
 });
 expect(decoded.decoded).toBe(true);expect(decoded.width).toBe(20);expect(decoded.height).toBe(10);
 expect(decoded.violations).toContain('https://untrusted.invalid/preview.png');
 expect(decoded.violations.some(uri=>uri.startsWith('blob:'))).toBe(false);
});
