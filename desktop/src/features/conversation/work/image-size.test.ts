import assert from 'node:assert/strict';
import {test} from 'node:test';
import {imageSize} from './stats.ts';
import {callStat,callTarget} from './target.ts';
import type {ToolStep} from '../types.ts';
const call=(args:string,output=''):ToolStep=>({id:'image-call',tool:'generate_image',hint:'',args,output,state:'done'});
test('image size uses only explicit positive safe pixel dimensions',()=>{
 for(const [size,want] of [['1024x1024','1024×1024'],['1536x1024','1536×1024'],[' 640 × 480 ','640×480']]) assert.equal(imageSize(JSON.stringify({size})),want);
 for(const size of ['','auto','16:9','1024','0x1024','-1x1024','1.5x10','Infinityx10','9007199254740992x10','10x20 extra',1024,null]) assert.equal(imageSize(JSON.stringify({size})),undefined);
 assert.equal(imageSize('{broken'),undefined);
});
test('the row displays provided image size and never invents dimensions from output or unknown fields',()=>{
 assert.deepEqual(callStat(call('{"prompt":"an icon set","size":"1024x1024"}')),{text:'1024×1024'});
 assert.deepEqual(callStat(call('{"prompt":"an icon set"}','Generated 1 image(s)\n1024x1024')),{});
 assert.deepEqual(callStat(call('{"width":1024,"height":1024,"dimensions":"1024x1024"}')),{});
 assert.deepEqual(callStat(call('{"size":"16:9"}')),{});
});
test('an explicit image prompt is quoted like the reference, without quoting an unknown prompt',()=>{
 assert.deepEqual(callTarget(call('{"prompt":"an icon set"}')),{kind:'text',text:'“an icon set”'});
 assert.ok(!JSON.stringify(callTarget(call('{"prompt":"   "}'))).includes('“'));
 assert.ok(!JSON.stringify(callTarget(call('{}'))).includes('“'));
});
