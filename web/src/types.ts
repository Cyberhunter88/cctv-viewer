import type {Recording} from './timeline';
export interface Camera {viewMode?:string;wideMarker?:string;zoomMarker?:string;zoomOffset?:number;supportsZoom?:boolean;id:string;name:string;folder:string;pattern:string;timezone:string;offset:number;format?:string;prefix?:string;indexingMode?:string;directoryPattern?:string;sourceStatus?:string;lastScan?:number;lastRecording?:number;indexedVideos?:number}
export interface ViewDetection {status:'confirmed'|'swapped'|'unclear';message:string;pairs:number}
export interface CameraScan {id:string;name:string;state:string;inspected:number;added:number;changed:number;skipped:number}
export interface Scan {jobId?:string;state?:string;completion?:number;queue?:{id:string;date:string;camera:string;automatic:boolean}[];cameras?:CameraScan[];active?:string[];completed?:number;total?:number;running:boolean;date:string;camera:string;indexed:number;warnings:string[]}
export interface Day {recordings:Recording[];from:number;to:number;scan:Scan}
