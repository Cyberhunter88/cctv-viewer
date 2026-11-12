import {test} from 'node:test';
import assert from 'node:assert/strict';
import {defaultView,validView,gridSize,pageCapacity,hotspotCandidate,recordingBoundary,moveCamera,validSavedViews,upsertSavedView,matchingSavedView,maxSavedViews} from './view-state.ts';
const records=[{id:'a',cameraId:'a',start:100,duration:50,codec:'h264'},{id:'b',cameraId:'b',start:120,duration:20,codec:'h264'}];
test('migrates old view preferences and sanitizes unknown cameras',()=>{assert.deepEqual(validView({cameras:['gone','a','a'],layout:'9',quality:'compatible',speed:'4'},['a']),{cameras:['a'],layout:'3x3',quality:'balanced',speed:'4'});assert.deepEqual(validView({cameras:[],layout:'100',quality:'bad',speed:'0',rows:99},['a']),{cameras:[]});assert.deepEqual(validView(null,[]),{})});
test('fixed, custom and narrow automatic grids preserve capacity',()=>{assert.deepEqual(gridSize('3x2',2,2,20,1200),[3,2]);assert.deepEqual(gridSize('custom',8,8,100,1200),[4,8]);assert.deepEqual(gridSize('auto',2,2,8,390),[1,2]);assert.deepEqual(gridSize('hotspot',2,2,6,1200),[3,2])});
test('hotspot chooses latest active start and camera order resolves ties',()=>{assert.equal(hotspotCandidate(records,['a','b'],119),'a');assert.equal(hotspotCandidate(records,['a','b'],120),'b');assert.equal(hotspotCandidate(records,['a','b'],150),undefined);assert.equal(hotspotCandidate([...records,{...records[1],id:'c',cameraId:'c'}],['c','b'],125),'c')});
test('boundary navigation includes ends, respects selection and avoids staying put',()=>{assert.equal(recordingBoundary(records,['a','b'],120,1),140);assert.equal(recordingBoundary(records,['a','b'],120,-1),100);assert.equal(recordingBoundary(records,['a'],151,-1),150);assert.equal(recordingBoundary(records,['b'],140,1),undefined)});
test('playing hotspot follows new starts, including short clips crossed between frames',()=>{assert.equal(hotspotCandidate(records,['a','b'],145,110),'b');assert.equal(hotspotCandidate(records,['a','b'],146,145),undefined)});
test('camera dragging moves forward after target and backward before target',()=>{assert.deepEqual(moveCamera(['a','b','c'],'a','c'),['b','c','a']);assert.deepEqual(moveCamera(['a','b','c'],'c','a'),['c','a','b']);assert.deepEqual(moveCamera(['a','b'],'gone','b'),['a','b'])});

test('six cameras retain readable capacity on phones and dual grids',()=>{
 assert.deepEqual(gridSize('auto',2,2,6,1732,true),[3,2]);
 assert.deepEqual(gridSize('auto',2,2,6,1304,true),[2,3]);
 assert.deepEqual(gridSize('3x2',2,2,6,320,true),[1,2]);
 assert.deepEqual(gridSize('custom',8,8,6,320),[1,2]);
 assert.equal(pageCapacity('hotspot',3,2),3);
 assert.equal(pageCapacity('hotspot',2,2),1);
 assert.equal(pageCapacity('hotspot',1,2),2);
 assert.deepEqual(validView({quality:'fast',aspect:'4/3'},[]),{quality:'fast',aspect:'4/3'});
});

test('personal preferences validate persisted input and retain old defaults',()=>{
 const state={...defaultView(),...validView({layout:'4',density:'compact',showNames:false,showOverview:false,showThumbnails:false,shortcuts:false,seekStep:30,timelineHeight:900},[])};
 assert.equal(state.layout,'2x2');assert.equal(state.showNames,false);assert.equal(state.showTimestamps,true);assert.equal(state.shortcuts,false);assert.equal(state.seekStep,30);assert.equal(state.timelineHeight,650);
 assert.deepEqual(validView({density:'tiny',showNames:'false',showCodec:0,seekStep:99,timelineHeight:'200'},[]),{});
 assert.equal(defaultView().showThumbnails,true);assert.equal(defaultView().seekStep,5);
});

test('rail preference and original migration version stay separate from legacy collapsed',()=>{
 assert.deepEqual(validView({collapsed:false,railCollapsed:true,originalQualityVersion:1},[]),{collapsed:false,railCollapsed:true,originalQualityVersion:1});
 assert.deepEqual(validView({railCollapsed:'false',originalQualityVersion:2},[]),{});
});
test('saved views drop malformed entries, duplicate names and removed cameras',()=>{
 const view={name:'Außen',cameras:['a','gone','a','b'],layout:'2x2',columns:2,rows:2};
 assert.deepEqual(validSavedViews([view,{...view,name:' Außen '},{...view,name:''},{...view,name:'x'.repeat(61)},{...view,name:'Layout',layout:'9x9'},{...view,name:'Raster',rows:9},{...view,name:'Kameras',cameras:[1]},null,'text'],['a','b']),[{name:'Außen',cameras:['a','b'],layout:'2x2',columns:2,rows:2}]);
 assert.deepEqual(validView({views:[view]},['a']).views,[{...view,cameras:['a']}]);
 assert.equal(validView({views:'x'},['a']).views,undefined);
 assert.equal(validSavedViews(Array.from({length:30},(_,i)=>({...view,name:'Ansicht '+i})),['a']).length,maxSavedViews);
});
test('saved views are replaced by name, limited in number and matched against the current state',()=>{
 const outside={name:'Außen',cameras:['a','b'],layout:'2x2',columns:2,rows:2},custom={name:'Raster',cameras:['a'],layout:'custom',columns:3,rows:1};
 assert.deepEqual(upsertSavedView([outside],custom),[outside,custom]);
 assert.deepEqual(upsertSavedView([outside,custom],{...outside,cameras:['b']}),[{...outside,cameras:['b']},custom]);
 const full=Array.from({length:maxSavedViews},(_,i)=>({...outside,name:'Ansicht '+i}));
 assert.equal(upsertSavedView(full,custom),undefined);
 assert.equal(upsertSavedView(full,{...custom,name:'Ansicht 3'})?.length,maxSavedViews);
 assert.equal(matchingSavedView([outside,custom],new Set(['b','a']),'2x2',4,4),'Außen');
 assert.equal(matchingSavedView([outside,custom],new Set(['a']),'2x2',2,2),'');
 assert.equal(matchingSavedView([outside,custom],new Set(['a','b']),'3x3',2,2),'');
 assert.equal(matchingSavedView([outside,custom],new Set(['a']),'custom',3,1),'Raster');
 assert.equal(matchingSavedView([outside,custom],new Set(['a']),'custom',3,2),'');
 assert.equal(matchingSavedView([],new Set(),'auto',2,2),'');
});
