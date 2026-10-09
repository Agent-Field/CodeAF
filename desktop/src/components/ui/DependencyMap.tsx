import { useLayoutEffect, useRef, useState, type ReactNode } from 'react';
export type DependencyEdge = { from: string; to: string; kind: 'feeds_into'|'blocks'|'suggests'; selected?: boolean };
type MeasuredEdge = DependencyEdge & { path: string };
/** Shared measured graph connectors, not UI glyph artwork. Nodes retain real DOM semantics. */
export function DependencyMap({ children, edges }: { children: ReactNode; edges: readonly DependencyEdge[] }) {
 const content = useRef<HTMLDivElement>(null);
 const [drawing,setDrawing]=useState<{width:number;height:number;edges:MeasuredEdge[]}>({width:0,height:0,edges:[]});
 useLayoutEffect(()=>{
  const host=content.current;if(!host)return;
  let frame=0;
  const measure=()=>{
   const bounds=host.getBoundingClientRect();const anchors=new Map<string,DOMRect>();
   const gap=Number.parseFloat(getComputedStyle(host).getPropertyValue('--space-3')) || 0;
   host.querySelectorAll<HTMLElement>('[data-graph-node]').forEach(node=>anchors.set(node.dataset.graphNode??'',node.getBoundingClientRect()));
   const paths=edges.flatMap(edge=>{
    const from=anchors.get(edge.from),to=anchors.get(edge.to);if(!from||!to)return[];
    const x1=from.right-bounds.left,y1=from.top+from.height/2-bounds.top,x2=to.left-bounds.left,y2=to.top+to.height/2-bounds.top;
    const bend=(x1+x2)/2;
    const obstacles=Array.from(anchors.entries()).filter(([id,box])=>{
     if(id===edge.from||id===edge.to||box.left<=from.right||box.right>=to.left)return false;
     return Array.from({length:17},(_,index)=>index/16).some(t=>{
      const inverse=1-t;
      const x=inverse**3*x1+3*inverse**2*t*bend+3*inverse*t**2*bend+t**3*x2+bounds.left;
      const y=inverse**3*y1+3*inverse**2*t*y1+3*inverse*t**2*y2+t**3*y2+bounds.top;
      return x>=box.left-gap&&x<=box.right+gap&&y>=box.top-gap&&y<=box.bottom+gap;
     });
    }).map(([,box])=>box);
    // A skipped stage must visibly bypass its unrelated node, rather than disappear behind it.
    const lane=obstacles.length ? Math.max(...obstacles.map(box=>box.bottom-bounds.top))+gap : undefined;
    const path=lane===undefined ? `M ${x1} ${y1} C ${bend} ${y1}, ${bend} ${y2}, ${x2} ${y2}` : `M ${x1} ${y1} C ${x1+gap} ${y1}, ${x1+gap} ${lane}, ${x1+gap*2} ${lane} L ${x2-gap*2} ${lane} C ${x2-gap} ${lane}, ${x2-gap} ${y2}, ${x2} ${y2}`;
    return[{...edge,path}];
   });
   const next={width:host.offsetWidth,height:host.offsetHeight,edges:paths};
   setDrawing(previous=>JSON.stringify(previous)===JSON.stringify(next)?previous:next);
  };
  const schedule=()=>{cancelAnimationFrame(frame);frame=requestAnimationFrame(measure);};
  const observer=new ResizeObserver(schedule);observer.observe(host);host.querySelectorAll('[data-graph-node]').forEach(node=>observer.observe(node));
  schedule();return()=>{observer.disconnect();cancelAnimationFrame(frame);};
 },[children,edges]);
 return <div ref={content} className="dependency-map"><svg className="dependency-map-edges" width={drawing.width} height={drawing.height} viewBox={`0 0 ${drawing.width || 1} ${drawing.height || 1}`} aria-hidden="true" focusable="false">{drawing.edges.map((edge,index)=><path key={`${edge.from}:${edge.to}:${edge.kind}:${index}`} d={edge.path} data-kind={edge.kind} data-selected={edge.selected || undefined}/>)}</svg>{children}</div>;
}
