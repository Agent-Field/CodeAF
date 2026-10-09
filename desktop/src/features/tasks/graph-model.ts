import type { PlanTaskRow, PlanDependency } from './plan-model';
export type { PlanDependency } from './plan-model';
export const dependencyWords = {feeds_into:'Feeds into',blocks:'Blocks',suggests:'Suggests'} as const;
/** Rank only hard, explicit dependency edges. Containment/advisories never create prerequisites. */
export function dependencyStages(rows:readonly PlanTaskRow[],dependencies:readonly PlanDependency[]):Map<string,number> {
 const ids=new Set(rows.map(row=>row.ID));const levels=new Map(rows.map(row=>[row.ID,0]));
 const hard=dependencies.filter(edge=>edge.kind!=='suggests'&&ids.has(edge.from)&&ids.has(edge.to)&&edge.from!==edge.to);
 const indegree=new Map(rows.map(row=>[row.ID,0]));hard.forEach(edge=>indegree.set(edge.to,(indegree.get(edge.to)??0)+1));
 const queue=rows.filter(row=>!indegree.get(row.ID)).map(row=>row.ID);
 for(let index=0;index<queue.length;index++){
  const id=queue[index];for(const edge of hard.filter(candidate=>candidate.from===id)){
   levels.set(edge.to,Math.max(levels.get(edge.to)??0,(levels.get(id)??0)+1));
   const count=(indegree.get(edge.to)??0)-1;indegree.set(edge.to,count);if(!count)queue.push(edge.to);
  }
 }
 // A cycle has no topological stage. Keep those nodes reachable in the first column.
 indegree.forEach((count,id)=>{if(count>0)levels.set(id,0);});return levels;
}
