import {Log,LogLevel} from 'miniflare';

// Runtime failures need bounded diagnostics without signed headers or payloads.
export class FixtureLog extends Log {
 constructor(write=message=>console.error(message)){super(LogLevel.WARN);this.write=write;this.lines=[];this.bytes=0;}
 capture(level,message){
  if(!/warn|error|fatal|^log$/i.test(String(level)))return;
  if(this.lines.length>=64||this.bytes>=16384)return;
  const text=String(message).replace(/((?:codeaf-[\w-]+|authorization|cookie|body|payload|signature|seed|private[_-]?key)["']?\s*[=:]\s*)[\s\S]*/i,'$1[redacted]').replace(/https?:\/\/[^\s)]+/g,'[url]').replace(/((?:codeaf-[\w-]+|authorization|cookie|body|payload|signature|seed|private[_-]?key)["']?\s*[=:]\s*)[^\n,]+/gi,'$1[redacted]').replace(/[A-Za-z0-9+/_=-]{48,}/g,'[redacted]').replace(/\x1b\[[0-9;]*m/g,'').slice(0,Math.min(2048,16384-this.bytes));
  const line='hosted runtime '+level+': '+text;this.lines.push(line);this.bytes+=text.length;this.write(line);
 }
 logWithLevel(level,message){if(level<=LogLevel.WARN)this.capture(level===LogLevel.ERROR?'error':'warn',message);}
}
