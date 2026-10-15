import assert from 'node:assert/strict';
import fs from 'node:fs/promises';
import http from 'node:http';
import path from 'node:path';
import {spawn,spawnSync} from 'node:child_process';

// Isolated fixture-only client: no real credentials, upstream model or images.
const binary = process.env.TOKENPRO_CODEX_BINARY ?? '/Applications/ChatGPT.app/Contents/Resources/codex-cli/bin/codex';
const fixtureRoot = await fs.mkdtemp(path.join(process.env.TOKENPRO_RUNTIME_WORK ?? process.cwd(), 'image-runtime-'));
const adapter = process.env.TOKENPRO_FIXTURE_ADAPTER;
const bundled=spawnSync(binary,['debug','models','--bundled'],{env:{...process.env,CODEX_HOME:fixtureRoot},encoding:'utf8',maxBuffer:16e6});
assert.equal(bundled.status,0);
const profile = JSON.parse(bundled.stdout).models.find(item => item.slug === 'gpt-6-luna');
assert(profile);
profile.slug='tp-g69-gpt-6-luna';
profile.use_responses_lite=false;
await fs.writeFile(path.join(fixtureRoot, 'catalog.json'), JSON.stringify({models:[profile]}));

async function probe(mode) {
  const requests = [];
  const server = http.createServer(async (req, res) => {
    let raw = ''; for await (const chunk of req) raw += chunk;
    if(req.method !== 'POST') {res.end(JSON.stringify({data:[]})); return;}
    let body = JSON.parse(raw);
    if(mode === 'declared' && adapter){
      const adapted=await fetch(adapter,{method:'POST',headers:{'Content-Type':'application/json'},body:raw,signal:AbortSignal.timeout(5000)});
      assert.equal(adapted.status,200);
      body=await adapted.json();
      assert.deepEqual(body.tools,JSON.parse(raw).tools,'adapter must preserve the real client tool registry');
      if(requests.length === 0 && !body.instructions?.includes('[TokenPro text image tool compatibility v1]')) {
        const snippets=(body.input??[]).filter(item=>item.role==='developer').map(item=>JSON.stringify(item.content)).filter(text=>text.includes('TokenPro')).map(text=>{const i=text.indexOf('TokenPro');return text.slice(Math.max(0,i-220),i+500);});
        throw new Error('actual client Skill catalog not recognized: '+JSON.stringify(snippets));
      }
      assert(body.instructions?.includes('[TokenPro fast image delivery v1]'));
      assert(body.instructions.includes('yield_time_ms: 30000'));
      assert(body.instructions.includes('exit code 0 and an actual saved absolute image path'));
      assert.deepEqual(body.reasoning,JSON.parse(raw).reasoning,'image delivery must not override user reasoning effort');
    }
    requests.push(body);
    let item;
    if(mode === 'unsupported' && requests.length === 1) {
      item = {id:'fc_probe',type:'function_call',call_id:'call_probe',namespace:'image_gen',name:'imagegen',arguments:'{"prompt":"fixture only; never generate"}'};
    } else if(mode === 'declared' && requests.length === 1) {
      item = {id:'ctc_probe',type:'custom_tool_call',call_id:'call_probe',namespace:'functions',name:'exec',input:'const result = await tools.exec_command({cmd:"/usr/bin/true",yield_time_ms:1000,max_output_tokens:32}); text(result.exit_code);'};
    } else {
      item = {id:'msg_probe',type:'message',role:'assistant',status:'completed',content:[{type:'output_text',text:'Fixture completed.',annotations:[]}]};
    }
    const response = {id:'resp_probe_'+requests.length,object:'response',status:'completed',model:body.model,output:[item],usage:{input_tokens:0,output_tokens:0,total_tokens:0}};
    const added={...item};
    if(item.type === 'function_call') added.arguments='';
    if(item.type === 'custom_tool_call') added.input='';
    const events=[{type:'response.created',response:{...response,status:'in_progress',output:[]}},
      {type:'response.output_item.added',output_index:0,item:added}];
    if(item.type === 'function_call') events.push({type:'response.function_call_arguments.delta',output_index:0,item_id:item.id,delta:item.arguments},{type:'response.function_call_arguments.done',output_index:0,item_id:item.id,arguments:item.arguments});
    if(item.type === 'custom_tool_call') events.push({type:'response.custom_tool_call_input.delta',output_index:0,item_id:item.id,delta:item.input},{type:'response.custom_tool_call_input.done',output_index:0,item_id:item.id,input:item.input});
    events.push({type:'response.output_item.done',output_index:0,item},{type:'response.completed',response});
    res.writeHead(200,{'Content-Type':'text/event-stream'});
    for(const event of events)res.write('data: '+JSON.stringify(event)+'\n\n');
    res.end();
  });
  await new Promise(resolve=>server.listen(0,'127.0.0.1',resolve));
  const fixtureHome=path.join(fixtureRoot,mode); await fs.mkdir(fixtureHome);
  await fs.mkdir(path.join(fixtureHome,'skills','tokenpro-imagegen'),{recursive:true});
  await fs.writeFile(path.join(fixtureHome,'skills','tokenpro-imagegen','SKILL.md'),'---\nname: tokenpro-imagegen\ndescription: Generate raster images through TokenPro using an installed CLI.\n---\nFixture only: never generate an image.\n');
  await fs.writeFile(path.join(fixtureHome,'config.toml'),`model="${profile.slug}"\nmodel_provider="openai"\nopenai_base_url="http://127.0.0.1:${server.address().port}/v1"\nmodel_catalog_json=${JSON.stringify(path.join(fixtureRoot,'catalog.json'))}\n`);
  await fs.writeFile(path.join(fixtureHome,'auth.json'),JSON.stringify({auth_mode:'apikey',OPENAI_API_KEY:'fixture-only'}),{mode:0o600});
  const child=spawn(binary,['app-server'],{env:{...process.env,CODEX_HOME:fixtureHome},stdio:['pipe','pipe','pipe']});
  let sequence=0,pending=new Map(),buffer='',diagnostics='',complete;
  const completed=new Promise(resolve=>complete=resolve);
  child.stderr.on('data',chunk=>diagnostics+=chunk);
  child.on('exit',code=>complete({status:'process_exit',code}));
  child.stdout.on('data',chunk=>{
    buffer+=chunk;const lines=buffer.split('\n');buffer=lines.pop();
    for(const line of lines){if(!line.trim())continue;const event=JSON.parse(line);
      if(event.id !== undefined && pending.has(event.id)){pending.get(event.id)(event);pending.delete(event.id);}
      if(event.method === 'turn/completed')complete(event.params.turn);
    }
  });
  const call=(method,params)=>new Promise(resolve=>{const id=++sequence;pending.set(id,resolve);child.stdin.write(JSON.stringify({id,method,params})+'\n');});
  const timeout=setTimeout(()=>{complete({status:'timeout'});child.kill();},25000);
  try {
    const initialized=await call('initialize',{clientInfo:{name:'image_runtime_probe',version:'1'},capabilities:{experimentalApi:true}});
    assert(!initialized.error,JSON.stringify(initialized.error));
    child.stdin.write(JSON.stringify({method:'initialized'})+'\n');
    const started=await call('thread/start',{model:profile.slug,modelProvider:'openai',cwd:fixtureRoot,approvalPolicy:'never',sandbox:'read-only',ephemeral:true,config:{'features.plugins':false},baseInstructions:'Execute only the fixture tool call and then finish. No images or network calls.'});
    assert(!started.error,JSON.stringify(started.error));
    const turn=await call('turn/start',{threadId:started.result.thread.id,input:[{type:'text',text:'Run the harmless fixture.'}]});
    assert(!turn.error,JSON.stringify(turn.error));
    const result=await completed;
    const toolOutputs=requests.flatMap(r=>(r.input??[]).filter(item=>/tool_call_output|function_call_output/.test(item.type)).map(item=>item.output));
    const initialTools=(requests[0]?.tools??[]).map(t=>({type:t.type,name:t.name,tools:t.tools?.map(x=>({type:x.type,name:x.name}))}));
    const unsupported=toolOutputs.some(output=>JSON.stringify(output).includes('unsupported call'));
    const skillAdvertised=(requests[0]?.input??[]).some(item=>item.role==='developer'&&JSON.stringify(item.content).includes('TokenPro'));
    console.log(JSON.stringify({mode,status:result.status,request_count:requests.length,adapter_used:mode==='declared'&&!!adapter,skill_advertised:skillAdvertised,initial_tools:initialTools,unsupported,tool_outputs:toolOutputs}));
    assert.equal(result.status,'completed');
    if(mode === 'unsupported')assert.equal(unsupported,true);
    if(mode === 'declared'){assert.equal(unsupported,false);assert(toolOutputs.length>0);}
  } finally {clearTimeout(timeout);child.kill();server.closeAllConnections();await new Promise(resolve=>server.close(resolve));}
}

await probe('unsupported');
await probe('declared');
