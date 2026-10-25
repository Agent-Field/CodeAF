(function(){
  'use strict';

  var DATA = window.KNOWLEDGE_LAB || {};
  var VAULTS = DATA.vaults || {};
  var COMPARISON = DATA.comparison || null;
  var RESEARCH = DATA.research || null;

  var state = {
    exp: 'reconciled',
    view: 'home',
    noteId: null,
    folder: null,
    query: '',
    openFolders: {}
  };

  var el = {
    sidebar: document.getElementById('sidebar'),
    main: document.getElementById('main'),
    mainInner: document.getElementById('main-inner'),
    details: document.getElementById('details'),
    detailsInner: document.getElementById('details-inner'),
    tree: document.getElementById('folder-tree'),
    treeCount: document.getElementById('tree-count'),
    search: document.getElementById('search-input'),
    overlay: document.getElementById('overlay'),
    overlayTitle: document.getElementById('overlay-title'),
    overlayBody: document.getElementById('overlay-body'),
    overlayClose: document.getElementById('overlay-close'),
    live: document.getElementById('live')
  };

  /* ---------- helpers ---------- */
  function vault(){ return VAULTS[state.exp] || {sources:{},claims:[],notes:[],relations:[],inventory:{}}; }
  function notes(){ return vault().notes || []; }
  function claims(){ return vault().claims || []; }
  function sources(){ return vault().sources || {}; }
  function relations(){ return vault().relations || []; }

  function byId(arr, id){ for(var i=0;i<arr.length;i++){ if(arr[i].id===id) return arr[i]; } return null; }
  function noteById(id){ return byId(notes(), id); }
  function isNoteRoute(id){ return noteById(id) || (vault().topic_registry && vault().topic_registry.redirects && vault().topic_registry.redirects[id]); }
  function claimById(id){ return byId(claims(), id); }
  function sourceById(id){ return sources()[id] || null; }

  function clear(node){ while(node.firstChild) node.removeChild(node.firstChild); }
  function mk(tag, cls, text){
    var n = document.createElement(tag);
    if(cls) n.className = cls;
    if(text != null) n.textContent = String(text);
    return n;
  }
  function btn(cls, text, label){
    var b = document.createElement('button');
    b.type = 'button';
    if(cls) b.className = cls;
    if(text != null) b.textContent = String(text);
    if(label) b.setAttribute('aria-label', label);
    return b;
  }
  function articleText(paragraph){
    var fragment = document.createDocumentFragment();
    // Python span offsets count Unicode code points, rather than UTF-16 units.
    var chars = Array.from(paragraph.text || '');
    var cursor = 0;
    (paragraph.inline_links || []).forEach(function(span){
      if(span.start < cursor || span.end <= span.start || span.end > chars.length || !noteById(span.id)) return;
      fragment.appendChild(document.createTextNode(chars.slice(cursor, span.start).join('')));
      var link = document.createElement('a');
      link.className = 'wiki-link';
      link.href = '#' + encodeURIComponent(span.id);
      link.textContent = span.label != null ? span.label : chars.slice(span.start, span.end).join('');
      link.title = 'Open ' + noteById(span.id).title;
      fragment.appendChild(link);
      cursor = span.end;
    });
    fragment.appendChild(document.createTextNode(chars.slice(cursor).join('')));
    return fragment;
  }

  function announce(msg){ el.live.textContent = msg; }

  function escapeRegExp(s){ return s.replace(/[.*+?^${}()|[\]\\]/g, '\\$&'); }

  function highlight(text, q){
    var frag = document.createDocumentFragment();
    if(!q){ frag.appendChild(document.createTextNode(text)); return frag; }
    var re = new RegExp('(' + escapeRegExp(q) + ')', 'ig');
    var parts = String(text).split(re);
    for(var i=0;i<parts.length;i++){
      if(i % 2 === 1){
        var m = document.createElement('mark');
        m.textContent = parts[i];
        frag.appendChild(m);
      } else {
        frag.appendChild(document.createTextNode(parts[i]));
      }
    }
    return frag;
  }

  function noteBodyText(note){
    var parts = [];
    if(note.sections){
      note.sections.forEach(function(section){
        parts.push(section.heading || '');
        (section.paragraphs || []).forEach(function(p){parts.push(p.text);});
      });
      return parts.join(' ');
    }
    var cs = note.claims || [];
    for(var i=0;i<cs.length;i++){
      var c = claimById(cs[i]);
      if(c && c.text && c.status === 'active') parts.push(c.text);
    }
    return parts.join(' ');
  }

  function noteSnippet(note, q){
    var body = noteBodyText(note);
    if(!body) return '';
    if(q){
      var idx = body.toLowerCase().indexOf(q.toLowerCase());
      if(idx >= 0){
        var start = Math.max(0, idx - 40);
        var end = Math.min(body.length, idx + q.length + 80);
        return (start>0?'\u2026':'') + body.slice(start, end) + (end<body.length?'\u2026':'');
      }
    }
    return body.length > 140 ? body.slice(0,140) + '\u2026' : body;
  }

  function scopeOf(note){ return note.scope || 'unscoped'; }

  function folderPath(note){
    if(!note.folder || !note.folder.length) return ['Unfiled'];
    return note.folder;
  }

  /* ---------- folder tree ---------- */
  function buildTree(){
    var ns = notes();
    var tree = Object.create(null);
    for(var i=0;i<ns.length;i++){
      var path = folderPath(ns[i]);
      var top = path[0];
      var sub = path[1] || null;
      if(!tree[top]) tree[top] = {count:0, subs:Object.create(null), notes:[]};
      tree[top].count++;
      if(sub){
        if(!tree[top].subs[sub]) tree[top].subs[sub] = {count:0, notes:[]};
        tree[top].subs[sub].count++;
        tree[top].subs[sub].notes.push(ns[i]);
      } else {
        tree[top].notes.push(ns[i]);
      }
    }
    return tree;
  }

  function renderTree(){
    clear(el.tree);
    var tree = buildTree();
    var names = Object.keys(tree).sort();
    el.treeCount.textContent = notes().length;

    for(var i=0;i<names.length;i++){
      (function(name){
        var node = tree[name];
        var wrap = mk('div', 'tree-folder');
        if(state.openFolders[name]) wrap.classList.add('open');

        var head = btn('tree-folder-btn', null);
        head.setAttribute('aria-expanded', state.openFolders[name] ? 'true' : 'false');
        var caret = mk('span', 'tree-caret', '\u25B6');
        var nm = mk('span', 'tree-folder-name', name);
        var ct = mk('span', 'tree-folder-count', node.count);
        head.appendChild(caret); head.appendChild(nm); head.appendChild(ct);
        head.addEventListener('click', function(){
          state.openFolders[name] = !state.openFolders[name];
          renderTree();
        });
        wrap.appendChild(head);

        var children = mk('div', 'tree-children');
        var subNames = Object.keys(node.subs).sort();
        for(var s=0;s<subNames.length;s++){
          (function(subName){
            var sub = node.subs[subName];
            var subWrap = mk('div', 'tree-folder');
            var key = name + '/' + subName;
            if(state.openFolders[key]) subWrap.classList.add('open');
            var subHead = btn('tree-folder-btn', null);
            subHead.setAttribute('aria-expanded', state.openFolders[key] ? 'true' : 'false');
            var sc = mk('span', 'tree-caret', '\u25B6');
            var sn = mk('span', 'tree-folder-name', subName);
            var sct = mk('span', 'tree-folder-count', sub.count);
            subHead.appendChild(sc); subHead.appendChild(sn); subHead.appendChild(sct);
            subHead.addEventListener('click', function(){
              state.openFolders[key] = !state.openFolders[key];
              renderTree();
            });
            subWrap.appendChild(subHead);
            var subChildren = mk('div', 'tree-children');
            for(var k=0;k<sub.notes.length;k++){
              subChildren.appendChild(noteTreeBtn(sub.notes[k]));
            }
            subWrap.appendChild(subChildren);
            children.appendChild(subWrap);
          })(subNames[s]);
        }
        for(var j=0;j<node.notes.length;j++){
          children.appendChild(noteTreeBtn(node.notes[j]));
        }
        wrap.appendChild(children);
        el.tree.appendChild(wrap);
      })(names[i]);
    }
  }

  function noteTreeBtn(note){
    var b = btn('tree-note', note.title || note.id);
    if(state.noteId === note.id) b.classList.add('active');
    b.addEventListener('click', function(){ openNote(note.id); });
    return b;
  }

  /* ---------- main render dispatch ---------- */
  function render(){
    document.querySelectorAll('.exp-btn').forEach(function(button){
      var selected = button.getAttribute('data-exp') === state.exp;
      button.setAttribute('aria-pressed', selected ? 'true' : 'false');
      button.classList.toggle('active', selected);
    });
    renderTree();
    updateNavActive();
    if(state.view === 'home') renderHome();
    else if(state.view === 'compare') renderCompare();
    else if(state.view === 'research') renderResearch();
    else if(state.view === 'search') renderSearch();
    else if(state.view === 'note') renderNote();
    else renderHome();
  }

  function updateNavActive(){
    var items = document.querySelectorAll('.nav-item');
    for(var i=0;i<items.length;i++){
      var v = items[i].getAttribute('data-view');
      if(v === state.view) items[i].classList.add('active');
      else items[i].classList.remove('active');
    }
  }

  /* ---------- Home ---------- */
  function renderHome(){
    clear(el.mainInner);
    var ns = notes();
    var cs = claims();
    var srcs = sources();
    var srcCount = Object.keys(srcs).length;

    var hero = mk('div', 'home-hero');
    hero.appendChild(mk('h1', null, 'Knowledge lab'));
    hero.appendChild(mk('p', null, 'Browse the ' + state.exp + ' experiment. Each statement links to recorded source material; agent reports still need verification.'));
    el.mainInner.appendChild(hero);

    var stats = mk('div', 'stat-grid');
    stats.appendChild(statCard(ns.length, 'Notes'));
    stats.appendChild(statCard(cs.filter(function(c){return c.status === 'active';}).length, 'Current statements'));
    stats.appendChild(statCard(srcCount, 'Sources'));
    stats.appendChild(statCard(ns.reduce(function(n,note){return n + (note.links || []).length;},0), 'Links'));
    el.mainInner.appendChild(stats);

    // scope cards
    var scopes = Object.create(null);
    for(var i=0;i<ns.length;i++){
      var s = scopeOf(ns[i]);
      if(!scopes[s]) scopes[s] = 0;
      scopes[s]++;
    }
    var scopeNames = Object.keys(scopes).sort();
    if(scopeNames.length){
      el.mainInner.appendChild(mk('h2', 'section-h', 'Scopes'));
      var sg = mk('div', 'scope-grid');
      for(var j=0;j<scopeNames.length;j++){
        (function(name){
          var card = btn('scope-card', null);
          card.appendChild(mk('div', 'scope-name', name));
          card.appendChild(mk('div', 'scope-count', scopes[name] + ' note' + (scopes[name]===1?'':'s')));
          card.addEventListener('click', function(){
            state.query = '';
            el.search.value = '';
            state.view = 'search';
            state.query = name;
            el.search.value = name;
            render();
          });
          sg.appendChild(card);
        })(scopeNames[j]);
      }
      el.mainInner.appendChild(sg);
    }

    // coverage
    var withEvidence = 0;
    for(var k=0;k<cs.length;k++){
      if(cs[k].evidence && cs[k].evidence.length) withEvidence++;
    }
    var cov = cs.length ? Math.round((withEvidence/cs.length)*100) : 0;
    el.mainInner.appendChild(mk('h2', 'section-h', 'Evidence coverage'));
    var covWrap = mk('div', 'cmp-card');
    covWrap.appendChild(mk('h3', null, cs.length ? withEvidence + ' statements link to source material' : 'No statements extracted yet'));
    var bar = mk('div', 'coverage-bar');
    var fill = mk('div', 'coverage-fill');
    fill.style.width = cov + '%';
    bar.appendChild(fill);
    if(cs.length) covWrap.appendChild(bar);
    covWrap.appendChild(mk('p', 'panel-sub', 'A source link establishes provenance, not independent verification.'));
    el.mainInner.appendChild(covWrap);

    // note previews
    el.mainInner.appendChild(mk('h2', 'section-h', 'Explore notes'));
    var list = mk('div', 'preview-list');
    var overview = ns.filter(function(n){return n.kind === 'map';}).concat(ns.filter(function(n){return n.kind === 'subject';}));
    var preview = (overview.length ? overview : ns).slice(0, 12);
    for(var p=0;p<preview.length;p++){
      (function(note){
        var item = btn('preview-item', null);
        item.appendChild(mk('div', 'preview-title', note.title || note.id));
        var snip = noteSnippet(note, '');
        if(snip) item.appendChild(mk('div', 'preview-snippet', snip));
        var meta = mk('div', 'preview-meta', scopeOf(note) + ' \u00b7 ' + (note.claims ? note.claims.length : 0) + ' claims');
        item.appendChild(meta);
        item.addEventListener('click', function(){ openNote(note.id); });
        list.appendChild(item);
      })(preview[p]);
    }
    if(!preview.length){
      list.appendChild(mk('div', 'detail-empty', 'No notes in this experiment yet.'));
    }
    el.mainInner.appendChild(list);

    renderDetailsHome();
  }

  function statCard(num, label){
    var c = mk('div', 'stat-card');
    c.appendChild(mk('div', 'stat-num', num));
    c.appendChild(mk('div', 'stat-label', label));
    return c;
  }

  /* ---------- Note view ---------- */
  function openNote(id){
    state.noteId = id;
    state.view = 'note';
    if(location.hash !== '#' + id) location.hash = id;
    render();
    el.main.scrollTop = 0;
    announce('Opened note ' + (noteById(id) ? noteById(id).title : id));
  }

  function renderNote(){
    clear(el.mainInner);
    var note = noteById(state.noteId);
    if(!note){
      var redirects = vault().topic_registry ? vault().topic_registry.redirects || {} : {};
      var targets = [], queue = [state.noteId], visited = {};
      while(queue.length){
        var candidate = queue.shift();
        if(visited[candidate]) continue;
        visited[candidate] = true;
        if(noteById(candidate)) targets.push(candidate);
        else (redirects[candidate] || []).forEach(function(t){queue.push(t);});
      }
      if(targets.length === 1){ openNote(targets[0]); return; }
      if(targets.length){
        el.mainInner.appendChild(mk('h1','note-title','This topic was reorganized'));
        targets.forEach(function(t){ var choice = btn('chip',noteById(t).title); choice.addEventListener('click',function(){openNote(t);}); el.mainInner.appendChild(choice); });
        renderDetailsEmpty(); return;
      }
      el.mainInner.appendChild(mk('h1', 'note-title', 'Note not found'));
      el.mainInner.appendChild(mk('p', null, 'This note is not present in the ' + state.exp + ' experiment.'));
      renderDetailsEmpty();
      return;
    }

    var crumb = mk('div', 'crumb');
    var homeBtn = btn(null, 'Home');
    homeBtn.addEventListener('click', function(){ state.view='home'; state.noteId=null; location.hash=''; render(); });
    crumb.appendChild(homeBtn);
    crumb.appendChild(document.createTextNode(' / '));
    var path = folderPath(note);
    crumb.appendChild(document.createTextNode(path.join(' / ')));
    el.mainInner.appendChild(crumb);

    el.mainInner.appendChild(mk('h1', 'note-title', note.title || note.id));
    if(note.human_edited){
      el.mainInner.appendChild(mk('p', 'detail-empty', 'Edited by you. Original source history is kept separately and does not verify your changes.'));
    }

    var meta = mk('div', 'note-meta');
    meta.appendChild(mk('span', null, scopeOf(note)));
    meta.appendChild(mk('span', 'dot', '\u00b7'));
    meta.appendChild(mk('span', null, note.human_edited ? 'Human edit' : (note.claims ? note.claims.length : 0) + ' claims'));
    if(note.links && note.links.length){
      meta.appendChild(mk('span', 'dot', '\u00b7'));
      meta.appendChild(mk('span', null, note.links.length + ' linked notes'));
    }
    el.mainInner.appendChild(meta);

    var cs = note.claims || [];
    if(note.sections){
      if(note.kind === 'map') el.mainInner.appendChild(mk('p','detail-empty','Browse subjects in this collection. These links organize reading; they do not assert factual relationships.'));
      if(!note.sections.length && note.kind !== 'map') el.mainInner.appendChild(mk('p', 'detail-empty', 'This note needs an editorial pass. Its sources and history are available on the right.'));
      var contents = note.sections.length >= 4 ? mk('nav', 'article-contents') : null;
      if(contents){ contents.setAttribute('aria-label', 'On this page'); contents.appendChild(mk('h3', null, 'On this page')); el.mainInner.appendChild(contents); }
      note.sections.forEach(function(section){
        if(section.heading){
          var heading = mk('h2', 'article-heading', section.heading);
          el.mainInner.appendChild(heading);
          if(contents){
            var jump = btn('text-btn', section.heading, 'Go to ' + section.heading);
            jump.addEventListener('click', function(){ heading.scrollIntoView({block:'start'}); });
            contents.appendChild(jump);
          }
        }
        (section.paragraphs || []).forEach(function(paragraph){
          var p = mk('p', 'article-paragraph');
          p.appendChild(articleText(paragraph));
          if(!note.human_edited && ((paragraph.claims || []).length || (paragraph.evidence || []).length)){
            var citation = btn('article-citation', 'Sources', 'Open supporting sources');
            citation.addEventListener('click', function(){openParagraphSources(paragraph.claims || [], paragraph.evidence || []);});
            p.appendChild(document.createTextNode(' '));
            p.appendChild(citation);
          }
          var basis = paragraph.evidence_basis;
          if(basis){
            var roles = basis.source_roles || [];
            var kinds = basis.claim_kinds || [];
            var label = basis.unresolved_conflict ? 'Unresolved conflict' :
              (kinds.length && kinds.every(function(k){ return k === 'proposal'; }) ? 'Proposed' :
              (roles.length === 1 && roles[0] === 'agent' ? 'Reported · not independently checked' : ''));
            if(label){
              var badge = mk('span', 'backlink-reason evidence-basis', label);
              badge.title = 'Source roles: ' + (roles.join(', ') || 'unknown') + '. Source kinds: ' + ((basis.source_kinds || []).join(', ') || 'unknown') + '. Observed: ' + (basis.observed_from || 'unknown') + ' to ' + (basis.observed_to || 'unknown') + '. A citation or model review does not independently verify a statement.';
              p.appendChild(document.createTextNode(' ')); p.appendChild(badge);
            }
          }
          if(paragraph.partial) p.appendChild(mk('span', 'backlink-reason', 'Partial evidence — this question remains open.'));
          el.mainInner.appendChild(p);
        });
      });
    } else if(!cs.length){
      el.mainInner.appendChild(mk('p', 'detail-empty', 'This note has no claims.'));
    }
    if(note.questions && note.questions.length){
      var pending = note.questions.filter(function(q){return q.state !== 'answered' && q.state !== 'retired_evidence';});
      var focused = pending.filter(function(q){return q.curiosity ? q.curiosity.state === 'active' : q.state !== 'backlog';});
      var parked = pending.filter(function(q){return q.curiosity ? q.curiosity.state !== 'active' : q.state === 'backlog';});
      function questionRow(question){
        var row = mk('p', 'article-paragraph', question.question);
        if(question.why) row.appendChild(mk('span', 'backlink-reason', question.why));
        var sourceButton = btn('article-citation', question.related_claims ? 'Possible answer in existing knowledge' : 'Why this question?', 'Open relevant evidence');
        sourceButton.addEventListener('click', function(){openParagraphSources((question.related_claims || []).concat(question.claims || []));});
        row.appendChild(sourceButton);
        return row;
      }
      if(focused.length){
        el.mainInner.appendChild(mk('h2', 'article-heading', 'Questions to understand next'));
        el.mainInner.appendChild(mk('p', 'detail-empty', 'These gaps remain unresolved in inspected evidence.'));
        focused.forEach(function(q){el.mainInner.appendChild(questionRow(q));});
      }
      if(parked.length){
        var backlog = mk('details', 'question-backlog');
        backlog.appendChild(mk('summary', null, parked.length + ' questions awaiting prioritization'));
        parked.forEach(function(q){backlog.appendChild(questionRow(q));});
        el.mainInner.appendChild(backlog);
      }
    }
    if(note.conflicts && note.conflicts.length){
      el.mainInner.appendChild(mk('h2', 'article-heading', 'Unresolved disagreements'));
      note.conflicts.forEach(function(conflict){
        var row = mk('p', 'article-paragraph', conflict.reason);
        var evidenceButton = btn('article-citation', 'Both sources', 'Inspect both sides of the disagreement');
        evidenceButton.addEventListener('click', function(){openParagraphSources(conflict.claims || []);});
        row.appendChild(evidenceButton);
        el.mainInner.appendChild(row);
      });
    }
    for(var i=0;!note.sections && i<cs.length;i++){
      var c = claimById(cs[i]);
      if(!c || c.status !== 'active') continue;
      el.mainInner.appendChild(renderClaim(c));
    }

    // linked notes
    if(note.links && note.links.length){
      var ls = mk('div', 'links-section');
      ls.appendChild(mk('h2', null, note.kind === 'map' ? 'Topics in this collection' : 'Linked notes'));
      var chips = mk('div', 'chips');
      for(var l=0;l<note.links.length;l++){
        (function(link){
          var target = noteById(link.id);
          var chip = btn('chip', target ? (target.title || link.id) : link.id);
          chip.title = link.reason || '';
          chip.addEventListener('click', function(){ openNote(link.id); });
          chips.appendChild(chip);
        })(note.links[l]);
      }
      ls.appendChild(chips);
      el.mainInner.appendChild(ls);
    }

    renderDetailsNote(note);
  }

  function renderClaim(c){
    var wrap = mk('div', 'claim' + (c.unsure ? ' unsure' : ''));
    var tags = mk('div', 'claim-tags');
    var kindLabels = {reported:'Agent reported',proposal:'Proposed',decision:'Decision',preference:'Preference',fact:'Stated fact'};
    if(c.kind) tags.appendChild(mk('span', 'tag kind-' + c.kind, kindLabels[c.kind] || c.kind));
    if(c.status) tags.appendChild(mk('span', 'tag status-' + c.status, c.status));
    if(c.topic) tags.appendChild(mk('span', 'tag', c.topic));
    if(c.scope) tags.appendChild(mk('span', 'tag', c.scope));
    wrap.appendChild(tags);
    wrap.appendChild(mk('p', 'claim-text', c.text));

    if(c.evidence && c.evidence.length){
      var ul = mk('ul', 'evidence-list');
      for(var i=0;i<c.evidence.length;i++){
        (function(ev){
          var li = mk('li');
          var b = btn('evidence-btn', null);
          b.appendChild(mk('span', 'evidence-quote', '\u201c' + ev.quote + '\u201d'));
          var src = sourceById(ev.source);
          b.appendChild(mk('span', 'evidence-src', src ? (src.role || 'source') + ' \u00b7 ' + (src.ts || '') : ev.source));
          b.addEventListener('click', function(){ openSource(ev.source, ev.quote); });
          li.appendChild(b);
          ul.appendChild(li);
        })(c.evidence[i]);
      }
      wrap.appendChild(ul);
    }
    return wrap;
  }

  /* ---------- Details pane ---------- */
  function renderDetailsHome(){
    clear(el.detailsInner);
    var sec = mk('div', 'detail-section');
    sec.appendChild(mk('h3', null, 'Experiment'));
    sec.appendChild(mk('p', 'detail-empty', 'Viewing the ' + state.exp + ' vault. Switch experiments in the sidebar to compare.'));
    el.detailsInner.appendChild(sec);

    var inv = vault().inventory || {};
    var keys = Object.keys(inv);
    if(keys.length){
      var invSec = mk('div', 'detail-section');
      invSec.appendChild(mk('h3', null, 'Inventory'));
      for(var i=0;i<keys.length;i++){
        var row = mk('div', 'context-row');
        row.appendChild(mk('span', 'k', keys[i]));
        row.appendChild(mk('span', 'v', readableValue(inv[keys[i]])));
        invSec.appendChild(row);
      }
      el.detailsInner.appendChild(invSec);
    }
  }

  function renderDetailsNote(note){
    clear(el.detailsInner);

    // backlinks
    var backSec = mk('div', 'detail-section');
    backSec.appendChild(mk('h3', null, 'Backlinks'));
    var backlinks = [];
    var ns = notes();
    for(var i=0;i<ns.length;i++){
      var links = ns[i].links || [];
      for(var j=0;j<links.length;j++){
        if(links[j].id === note.id){
          backlinks.push({note: ns[i], reason: links[j].reason});
        }
      }
    }
    if(backlinks.length){
      for(var b=0;b<backlinks.length;b++){
        (function(bl){
          var btnEl = btn('backlink-btn', bl.note.title || bl.note.id);
          if(bl.reason) btnEl.appendChild(mk('span', 'backlink-reason', bl.reason));
          btnEl.addEventListener('click', function(){ openNote(bl.note.id); });
          backSec.appendChild(btnEl);
        })(backlinks[b]);
      }
    } else {
      backSec.appendChild(mk('p', 'detail-empty', 'No other notes link here.'));
    }
    el.detailsInner.appendChild(backSec);

    // source quotes
    var quoteSec = mk('div', 'detail-section');
    quoteSec.appendChild(mk('h3', null, note.human_edited ? 'Original source history' : 'Source quotes'));
    if(note.human_edited) quoteSec.appendChild(mk('p', 'detail-empty', 'Historical material, not citations for the edited text.'));
    var quotes = note.human_edited ? (note.original_evidence || []).slice() : [];
    var cs = note.claims || [];
    for(var k=0;k<cs.length;k++){
      var c = claimById(cs[k]);
      if(c && c.evidence){
        for(var e=0;e<c.evidence.length;e++){
          quotes.push(c.evidence[e]);
        }
      }
    }
    if(quotes.length){
      for(var q=0;q<quotes.length;q++){
        (function(ev){
          var qb = btn('quote-btn', null);
          qb.appendChild(mk('span', 'q-text', '\u201c' + ev.quote + '\u201d'));
          var src = sourceById(ev.source);
          qb.appendChild(mk('span', 'q-src', src ? (src.role || 'source') + ' \u00b7 ' + (src.ts || '') : ev.source));
          qb.addEventListener('click', function(){ openSource(ev.source, ev.quote); });
          quoteSec.appendChild(qb);
        })(quotes[q]);
      }
    } else {
      quoteSec.appendChild(mk('p', 'detail-empty', 'No source quotes for this note.'));
    }
    el.detailsInner.appendChild(quoteSec);

    // history
    var histSec = mk('div', 'detail-section');
    histSec.appendChild(mk('h3', null, 'History'));
    var hist = (note.claims || []).map(claimById).filter(function(c){return c && c.status !== 'active';}).map(function(c){
      var reasons = relations().filter(function(r){return r.to === c.id || r.from === c.id;}).map(function(r){return r.reason || r.kind;});
      return {label:c.status,text:c.text,why:c.audit ? c.audit.reason : reasons.join(' · ')};
    });
    hist = hist.concat((note.article_rejections || []).map(function(p){return {label:'Editorial review',text:p.text,why:p.reason};}));
    if(hist.length){
      for(var h=0;h<hist.length;h++){
        (function(item){
          var hi = mk('div', 'history-item');
          hi.appendChild(mk('span', 'h-label', item.label || 'superseded'));
          hi.appendChild(mk('span', 'h-text', item.text || ''));
          if(item.why) hi.appendChild(mk('span', 'h-why', item.why));
          histSec.appendChild(hi);
        })(hist[h]);
      }
    } else {
      histSec.appendChild(mk('p', 'detail-empty', 'No superseded or duplicate lines recorded.'));
    }
    el.detailsInner.appendChild(histSec);

    // context
    var ctxSec = mk('div', 'detail-section');
    ctxSec.appendChild(mk('h3', null, 'Context'));
    var rows = [
      ['Scope', scopeOf(note)],
      ['Folder', folderPath(note).join(' / ')],
      ['Claims', (note.claims ? note.claims.length : 0)]
    ];
    if(note.date) rows.push(['Date', note.date]);
    for(var r=0;r<rows.length;r++){
      var row = mk('div', 'context-row');
      row.appendChild(mk('span', 'k', rows[r][0]));
      row.appendChild(mk('span', 'v', rows[r][1]));
      ctxSec.appendChild(row);
    }
    el.detailsInner.appendChild(ctxSec);

    // mini-map
    var mapSec = mk('div', 'detail-section');
    mapSec.appendChild(mk('h3', null, 'Connected notes'));
    mapSec.appendChild(buildMinimap(note));
    el.detailsInner.appendChild(mapSec);
  }

  function renderDetailsEmpty(){
    clear(el.detailsInner);
    el.detailsInner.appendChild(mk('p', 'detail-empty', 'Select a note to see backlinks, source quotes and history.'));
  }

  /* ---------- Mini-map ---------- */
  function buildMinimap(note){
    var ns = notes();
    var links = note.links || [];
    var backlinks = [];
    for(var i=0;i<ns.length;i++){
      var ls = ns[i].links || [];
      for(var j=0;j<ls.length;j++){
        if(ls[j].id === note.id) backlinks.push(ns[i]);
      }
    }
    var neighbors = [];
    for(var l=0;l<links.length;l++){
      var t = noteById(links[l].id);
      if(t) neighbors.push(t);
    }
    for(var b=0;b<backlinks.length;b++){
      var exists = false;
      for(var n=0;n<neighbors.length;n++){ if(neighbors[n].id === backlinks[b].id) exists = true; }
      if(!exists) neighbors.push(backlinks[b]);
    }
    if(!neighbors.length){
      return mk('p', 'detail-empty', 'No connected notes.');
    }

    var W = 280, H = 200, cx = W/2, cy = H/2, R = 70;
    var svg = document.createElementNS('http://www.w3.org/2000/svg', 'svg');
    svg.setAttribute('viewBox', '0 0 ' + W + ' ' + H);
    svg.setAttribute('class', 'minimap');
    svg.setAttribute('role', 'img');
    svg.setAttribute('aria-label', 'Connected notes map');

    var count = neighbors.length;
    for(var k=0;k<count;k++){
      var angle = (k/count) * Math.PI * 2 - Math.PI/2;
      var x = cx + Math.cos(angle) * R;
      var y = cy + Math.sin(angle) * R;

      var line = document.createElementNS('http://www.w3.org/2000/svg', 'line');
      line.setAttribute('x1', cx); line.setAttribute('y1', cy);
      line.setAttribute('x2', x); line.setAttribute('y2', y);
      line.setAttribute('class', 'mm-edge');
      svg.appendChild(line);

      (function(nb, nx, ny){
        var g = document.createElementNS('http://www.w3.org/2000/svg', 'g');
        g.setAttribute('class', 'mm-node');
        g.setAttribute('tabindex', '0');
        g.setAttribute('role', 'button');
        g.setAttribute('aria-label', 'Open ' + (nb.title || nb.id));
        var circle = document.createElementNS('http://www.w3.org/2000/svg', 'circle');
        circle.setAttribute('cx', nx); circle.setAttribute('cy', ny); circle.setAttribute('r', 7);
        g.appendChild(circle);
        var label = document.createElementNS('http://www.w3.org/2000/svg', 'text');
        label.setAttribute('x', nx); label.setAttribute('y', ny + 18);
        label.setAttribute('text-anchor', 'middle');
        var title = nb.title || nb.id;
        label.textContent = title.length > 14 ? title.slice(0,13) + '\u2026' : title;
        g.appendChild(label);
        g.addEventListener('click', function(){ openNote(nb.id); });
        g.addEventListener('keydown', function(ev){
          if(ev.key === 'Enter' || ev.key === ' '){ ev.preventDefault(); openNote(nb.id); }
        });
        svg.appendChild(g);
      })(neighbors[k], x, y);
    }

    var center = document.createElementNS('http://www.w3.org/2000/svg', 'circle');
    center.setAttribute('cx', cx); center.setAttribute('cy', cy); center.setAttribute('r', 9);
    center.setAttribute('fill', '#c98a3c');
    svg.appendChild(center);

    return svg;
  }

  /* ---------- Search ---------- */
  function renderSearch(){
    clear(el.mainInner);
    var q = state.query.trim();
    var ns = notes();
    var results = [];
    if(q){
      var lq = q.toLowerCase();
      for(var i=0;i<ns.length;i++){
        var title = ((ns[i].title || '') + ' ' + (ns[i].aliases || []).join(' ')).toLowerCase();
        var body = noteBodyText(ns[i]).toLowerCase();
        if(title.indexOf(lq) >= 0 || body.indexOf(lq) >= 0){
          results.push(ns[i]);
        }
      }
    }

    var head = mk('div', 'search-head');
    if(q){
      head.appendChild(document.createTextNode('Found '));
      head.appendChild(mk('strong', null, results.length));
      head.appendChild(document.createTextNode(' note' + (results.length===1?'':'s') + ' for \u201c' + q + '\u201d'));
    } else {
      head.textContent = 'Type to search note titles and body text.';
    }
    el.mainInner.appendChild(head);

    if(!q){
      el.mainInner.appendChild(mk('p', 'detail-empty', 'Search across all notes in the ' + state.exp + ' experiment.'));
      renderDetailsEmpty();
      return;
    }

    if(!results.length){
      el.mainInner.appendChild(mk('p', 'detail-empty', 'No notes match this search.'));
      renderDetailsEmpty();
      return;
    }

    var list = mk('div', 'preview-list');
    for(var r=0;r<results.length;r++){
      (function(note){
        var item = btn('preview-item', null);
        var titleEl = mk('div', 'preview-title');
        titleEl.appendChild(highlight(note.title || note.id, q));
        item.appendChild(titleEl);
        var snip = noteSnippet(note, q);
        if(snip){
          var snipEl = mk('div', 'preview-snippet');
          snipEl.appendChild(highlight(snip, q));
          item.appendChild(snipEl);
        }
        item.appendChild(mk('div', 'preview-meta', scopeOf(note) + ' \u00b7 ' + (note.claims ? note.claims.length : 0) + ' claims'));
        item.addEventListener('click', function(){ openNote(note.id); });
        list.appendChild(item);
      })(results[r]);
    }
    el.mainInner.appendChild(list);
    renderDetailsEmpty();
  }

  function readableValue(value){
    if(Array.isArray(value)) return value.map(readableValue).join(' · ');
    if(value && typeof value === 'object') return Object.keys(value).map(function(key){
      return key.replace(/_/g, ' ') + ': ' + readableValue(value[key]);
    }).join(' · ');
    return value == null ? 'Not recorded' : String(value);
  }

  function structuredCard(parent, label, value){
    var card = mk('section', 'cmp-card');
    card.appendChild(mk('h3', null, label.replace(/_/g, ' ')));
    if(Array.isArray(value)){
      value.forEach(function(item, index){
        if(item && typeof item === 'object') structuredCard(card, item.feature || item.name || item.title || String(index + 1), item);
        else card.appendChild(mk('p', null, readableValue(item)));
      });
    } else if(value && typeof value === 'object'){
      Object.keys(value).forEach(function(key){
        var item = value[key];
        if(item && typeof item === 'object' && !Array.isArray(item)){
          structuredCard(card, key, item);
        } else {
          var row = mk('div', 'context-row');
          row.appendChild(mk('span', 'k', key.replace(/_/g, ' ')));
          row.appendChild(mk('span', 'v', readableValue(item)));
          card.appendChild(row);
        }
      });
    } else card.appendChild(mk('p', null, readableValue(value)));
    parent.appendChild(card);
  }

  function renderCompare(){
    clear(el.mainInner);
    el.mainInner.appendChild(mk('h1', 'note-title', 'Compare experiments'));
    el.mainInner.appendChild(mk('p', 'panel-sub', 'Measured structure and recorded costs. Model audits are not human accuracy scores.'));
    if(!COMPARISON) el.mainInner.appendChild(mk('p', 'detail-empty', 'No comparison has been recorded.'));
    else Object.keys(COMPARISON).forEach(function(key){structuredCard(el.mainInner, key, COMPARISON[key]);});
    renderDetailsEmpty();
  }

  function renderResearch(){
    clear(el.mainInner);
    el.mainInner.appendChild(mk('h1', 'note-title', 'Algorithm ideas and designer brief'));
    el.mainInner.appendChild(mk('p', 'panel-sub', 'Research candidates, trade-offs, and proposed surfaces. These are not all implemented features.'));
    if(!RESEARCH) el.mainInner.appendChild(mk('p', 'detail-empty', 'No research brief has been recorded.'));
    else Object.keys(RESEARCH).forEach(function(key){structuredCard(el.mainInner, key, RESEARCH[key]);});
    renderDetailsEmpty();
  }

  /* ---------- Source overlay ---------- */
  function openParagraphSources(ids, directEvidence){
    clear(el.overlayBody);
    el.overlayTitle.textContent = 'Supporting sources';
    ids.forEach(function(id){
      var claim = claimById(id);
      if(!claim) return;
      (claim.evidence || []).forEach(function(evidence){
        var source = sourceById(evidence.source);
        el.overlayBody.appendChild(mk('blockquote', 'src-quote', evidence.quote));
        var button = btn('evidence-btn', source ? 'Open ' + source.role + ' message · ' + source.ts : 'Open source');
        button.addEventListener('click', function(){openSource(evidence.source, evidence.quote);});
        el.overlayBody.appendChild(button);
      });
    });
    (directEvidence || []).forEach(function(evidence){
      var source = sourceById(evidence.source);
      el.overlayBody.appendChild(mk('blockquote', 'src-quote', evidence.quote));
      var button = btn('evidence-btn', source ? 'Open ' + source.role + ' message · ' + source.ts : 'Open source');
      button.addEventListener('click', function(){openSource(evidence.source, evidence.quote);});
      el.overlayBody.appendChild(button);
    });
    el.overlay.hidden = false;
    el.overlayClose.focus();
  }

  function openSource(sourceId, quote){
    var src = sourceById(sourceId);
    clear(el.overlayBody);
    el.overlayTitle.textContent = 'Source message';

    if(!src){
      el.overlayBody.appendChild(mk('p', 'detail-empty', 'Source not found in this experiment.'));
    } else {
      var meta = mk('div', 'src-meta');
      if(src.role) meta.appendChild(mk('span', null, 'Role: ' + src.role));
      if(src.ts) meta.appendChild(mk('span', null, 'Time: ' + src.ts));
      if(src.session) meta.appendChild(mk('span', null, 'Session: ' + src.session));
      meta.appendChild(mk('span', null, 'ID: ' + src.id));
      el.overlayBody.appendChild(meta);

      var text = mk('div', 'src-text', src.text || '');
      el.overlayBody.appendChild(text);

      if(quote){
        var q = mk('div', 'src-quote', '\u201c' + quote + '\u201d');
        el.overlayBody.appendChild(q);
      }
    }

    el.overlay.hidden = false;
    el.overlayClose.focus();
  }

  function closeOverlay(){
    el.overlay.hidden = true;
    el.overlayBody.textContent = '';
  }

  /* ---------- events ---------- */
  function bindEvents(){
    // experiment switch
    var expBtns = document.querySelectorAll('.exp-btn');
    for(var i=0;i<expBtns.length;i++){
      (function(b){
        b.addEventListener('click', function(){
          var exp = b.getAttribute('data-exp');
          if(exp === state.exp) return;
          state.exp = exp;
          for(var j=0;j<expBtns.length;j++){
            expBtns[j].setAttribute('aria-pressed', expBtns[j] === b ? 'true' : 'false');
          }
          // preserve selection where possible
          if(state.noteId && !noteById(state.noteId)){
            state.noteId = null;
            if(state.view === 'note') state.view = 'home';
          }
          render();
          announce('Switched to ' + exp + ' experiment');
        });
      })(expBtns[i]);
    }

    // nav
    var navItems = document.querySelectorAll('.nav-item');
    for(var n=0;n<navItems.length;n++){
      (function(b){
        b.addEventListener('click', function(){
          state.view = b.getAttribute('data-view');
          state.noteId = null;
          location.hash = '';
          render();
        });
      })(navItems[n]);
    }

    // search
    var searchTimer = null;
    el.search.addEventListener('input', function(){
      clearTimeout(searchTimer);
      searchTimer = setTimeout(function(){
        state.query = el.search.value;
        if(state.query.trim()){
          state.view = 'search';
          state.noteId = null;
        } else if(state.view === 'search'){
          state.view = 'home';
        }
        render();
      }, 120);
    });

    // keyboard
    document.addEventListener('keydown', function(ev){
      if((ev.metaKey || ev.ctrlKey) && ev.key.toLowerCase() === 'k'){
        ev.preventDefault();
        el.search.focus();
        el.search.select();
      }
      if(ev.key === 'Escape'){
        if(!el.overlay.hidden){ closeOverlay(); }
      }
    });

    el.overlayClose.addEventListener('click', closeOverlay);
    el.overlay.addEventListener('click', function(ev){
      if(ev.target === el.overlay) closeOverlay();
    });

    // hash routing
    window.addEventListener('hashchange', function(){
      var id = location.hash.replace(/^#/, '');
      if(id && isNoteRoute(id)){
        state.noteId = id;
        state.view = 'note';
        render();
      }
    });
  }

  /* ---------- init ---------- */
  function init(){
    bindEvents();
    var id = location.hash.replace(/^#/, '');
    if(id && isNoteRoute(id)){
      state.noteId = id;
      state.view = 'note';
    }
    render();
  }

  if(document.readyState === 'loading'){
    document.addEventListener('DOMContentLoaded', init);
  } else {
    init();
  }
})();
