import { writeFileSync } from 'node:fs';
import { generatedFiles } from './design-output.mjs';
for (const [path, output] of Object.entries(generatedFiles)) writeFileSync(path, output);
console.log('Generated theme tokens and monochrome brand assets.');
