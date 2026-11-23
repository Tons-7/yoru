/* eslint-disable */

export class FhEngine {
    private constructor();
    free(): void;
    [Symbol.dispose](): void;
    autotune(model: FhModel, options: any): Promise<any>;
    compiledKernels(): string[];
    static create(options: any): Promise<FhEngine>;
    device(): any;
    info(): any;
    kernelSource(name: string): any;
    loadModel(graph: string, manifest: string, weights: Uint8Array): FhModel;
    precompile(names: string[]): number;
    report(): any;
    session(model: FhModel, options: any): FhSession;
    stream(model: FhModel, session: any, stream: any): FhStream;
}

export class FhModel {
    private constructor();
    free(): void;
    [Symbol.dispose](): void;
    info(): any;
}

export class FhPacer {
    free(): void;
    [Symbol.dispose](): void;
    costSample(now: number, stage: string, ms: number): boolean;
    delayMs(): number;
    factor(): number;
    generated(now: number, request: number, output: number): void;
    hostStall(now: number, ms: number): void;
    modeChange(now: number): void;
    constructor(options: any);
    options(): any;
    penalty(): number;
    record(host: string): void;
    report(): any;
    reset(now: number): void;
    setOptions(now: number, options: any): void;
    skipped(request: number): void;
    sourceFrame(now: number, frame: any): any;
    stopRecording(): any;
    tick(now: number): any;
    trace(): any;
}

export class FhSession {
    private constructor();
    free(): void;
    [Symbol.dispose](): void;
    addInput(): number;
    addOutput(): number;
    generate(a: number, b: number, t: number, out: number): void;
    inputTexture(slot: number): any;
    midTimed(t: number, out: number): Promise<any>;
    mid(t: number, out: number): void;
    outputTexture(slot: number): any;
    pairTimed(a: number, b: number): Promise<any>;
    pair(a: number, b: number): void;
    pushBitmap(slot: number, b: ImageBitmap): void;
    pushCanvas(slot: number, c: HTMLCanvasElement): void;
    pushVideo(slot: number, v: HTMLVideoElement): void;
    readRgba(out: number): Promise<any>;
    report(): any;
    warmUp(): void;
    writeMask(mask: Uint8Array): void;
    writeRgba(slot: number, rgba: Uint8Array): void;
}

export class FhStream {
    private constructor();
    free(): void;
    [Symbol.dispose](): void;
    addInput(): number;
    addOutput(): number;
    classify(previous: number, slot: number): Promise<any>;
    generateTimed(t: number, out: number): any;
    generate(t: number, out: number): boolean;
    inputTexture(slot: number): any;
    mouse(t: number, dx: number, dy: number): void;
    nextInput(in_use: any): number;
    options(): any;
    outputTexture(slot: number): any;
    pushBitmap(slot: number, b: ImageBitmap): void;
    pushCanvas(slot: number, c: HTMLCanvasElement): void;
    pushTimed(slot: number, pts: number): any;
    pushVideo(slot: number, v: HTMLVideoElement): void;
    push(slot: number, pts: number): any;
    readRgba(out: number): Promise<any>;
    realReplaced(): boolean;
    report(): any;
    reset(): void;
    setMouseCaptured(captured: boolean): void;
    stats(): Promise<any>;
    warmUp(): void;
    warp(output: boolean, slot: number, out: number, now: number): boolean;
    writeMask(mask: Uint8Array): void;
}

export function fh_pacer_replay(trace: any, options: any): any;

export type InitInput = RequestInfo | URL | Response | BufferSource | WebAssembly.Module;

export interface InitOutput {
    readonly memory: WebAssembly.Memory;
    readonly __wbg_fhengine_free: (a: number, b: number) => void;
    readonly __wbg_fhmodel_free: (a: number, b: number) => void;
    readonly __wbg_fhpacer_free: (a: number, b: number) => void;
    readonly __wbg_fhsession_free: (a: number, b: number) => void;
    readonly __wbg_fhstream_free: (a: number, b: number) => void;
    readonly fh_pacer_replay: (a: any, b: any) => [number, number, number];
    readonly fhengine_autotune: (a: number, b: number, c: any) => [number, number, number];
    readonly fhengine_compiledKernels: (a: number) => [number, number];
    readonly fhengine_create: (a: any) => any;
    readonly fhengine_device: (a: number) => [number, number, number];
    readonly fhengine_info: (a: number) => [number, number, number];
    readonly fhengine_kernelSource: (a: number, b: number, c: number) => [number, number, number];
    readonly fhengine_loadModel: (a: number, b: number, c: number, d: number, e: number, f: number, g: number) => [number, number, number];
    readonly fhengine_precompile: (a: number, b: number, c: number) => number;
    readonly fhengine_report: (a: number) => [number, number, number];
    readonly fhengine_session: (a: number, b: number, c: any) => [number, number, number];
    readonly fhengine_stream: (a: number, b: number, c: any, d: any) => [number, number, number];
    readonly fhmodel_info: (a: number) => [number, number, number];
    readonly fhpacer_costSample: (a: number, b: number, c: number, d: number, e: number) => [number, number, number];
    readonly fhpacer_delayMs: (a: number) => number;
    readonly fhpacer_factor: (a: number) => number;
    readonly fhpacer_generated: (a: number, b: number, c: number, d: number) => void;
    readonly fhpacer_hostStall: (a: number, b: number, c: number) => void;
    readonly fhpacer_modeChange: (a: number, b: number) => void;
    readonly fhpacer_new: (a: any) => [number, number, number];
    readonly fhpacer_options: (a: number) => [number, number, number];
    readonly fhpacer_penalty: (a: number) => number;
    readonly fhpacer_record: (a: number, b: number, c: number) => void;
    readonly fhpacer_report: (a: number) => [number, number, number];
    readonly fhpacer_reset: (a: number, b: number) => void;
    readonly fhpacer_setOptions: (a: number, b: number, c: any) => [number, number];
    readonly fhpacer_skipped: (a: number, b: number) => void;
    readonly fhpacer_sourceFrame: (a: number, b: number, c: any) => [number, number, number];
    readonly fhpacer_stopRecording: (a: number) => [number, number, number];
    readonly fhpacer_tick: (a: number, b: number) => [number, number, number];
    readonly fhpacer_trace: (a: number) => [number, number, number];
    readonly fhsession_addInput: (a: number) => number;
    readonly fhsession_addOutput: (a: number) => number;
    readonly fhsession_generate: (a: number, b: number, c: number, d: number, e: number) => [number, number];
    readonly fhsession_inputTexture: (a: number, b: number) => [number, number, number];
    readonly fhsession_mid: (a: number, b: number, c: number) => [number, number];
    readonly fhsession_midTimed: (a: number, b: number, c: number) => [number, number, number];
    readonly fhsession_outputTexture: (a: number, b: number) => [number, number, number];
    readonly fhsession_pair: (a: number, b: number, c: number) => [number, number];
    readonly fhsession_pairTimed: (a: number, b: number, c: number) => [number, number, number];
    readonly fhsession_pushBitmap: (a: number, b: number, c: any) => [number, number];
    readonly fhsession_pushCanvas: (a: number, b: number, c: any) => [number, number];
    readonly fhsession_pushVideo: (a: number, b: number, c: any) => [number, number];
    readonly fhsession_readRgba: (a: number, b: number) => [number, number, number];
    readonly fhsession_report: (a: number) => [number, number, number];
    readonly fhsession_warmUp: (a: number) => [number, number];
    readonly fhsession_writeMask: (a: number, b: number, c: number) => [number, number];
    readonly fhsession_writeRgba: (a: number, b: number, c: number, d: number) => [number, number];
    readonly fhstream_addInput: (a: number) => number;
    readonly fhstream_addOutput: (a: number) => number;
    readonly fhstream_classify: (a: number, b: number, c: number) => [number, number, number];
    readonly fhstream_generate: (a: number, b: number, c: number) => [number, number, number];
    readonly fhstream_generateTimed: (a: number, b: number, c: number) => [number, number, number];
    readonly fhstream_inputTexture: (a: number, b: number) => [number, number, number];
    readonly fhstream_mouse: (a: number, b: number, c: number, d: number) => void;
    readonly fhstream_nextInput: (a: number, b: any) => number;
    readonly fhstream_options: (a: number) => [number, number, number];
    readonly fhstream_outputTexture: (a: number, b: number) => [number, number, number];
    readonly fhstream_push: (a: number, b: number, c: number) => [number, number, number];
    readonly fhstream_pushBitmap: (a: number, b: number, c: any) => [number, number];
    readonly fhstream_pushCanvas: (a: number, b: number, c: any) => [number, number];
    readonly fhstream_pushTimed: (a: number, b: number, c: number) => [number, number, number];
    readonly fhstream_pushVideo: (a: number, b: number, c: any) => [number, number];
    readonly fhstream_readRgba: (a: number, b: number) => [number, number, number];
    readonly fhstream_realReplaced: (a: number) => number;
    readonly fhstream_report: (a: number) => [number, number, number];
    readonly fhstream_reset: (a: number) => void;
    readonly fhstream_setMouseCaptured: (a: number, b: number) => void;
    readonly fhstream_stats: (a: number) => any;
    readonly fhstream_warmUp: (a: number) => [number, number];
    readonly fhstream_warp: (a: number, b: number, c: number, d: number, e: number) => [number, number, number];
    readonly fhstream_writeMask: (a: number, b: number, c: number) => [number, number];
    readonly wasm_bindgen_388cfc06d0305505___convert__closures_____invoke___js_sys_db7a8120fbf7a135___Function_fn_wasm_bindgen_388cfc06d0305505___JsValue_____wasm_bindgen_388cfc06d0305505___sys__Undefined___js_sys_db7a8120fbf7a135___Function_fn_wasm_bindgen_388cfc06d0305505___JsValue_____wasm_bindgen_388cfc06d0305505___sys__Undefined_______true_: (a: number, b: number, c: any, d: any) => void;
    readonly wasm_bindgen_388cfc06d0305505___convert__closures_____invoke___wasm_bindgen_388cfc06d0305505___sys__JsNullable_wgpu_f5334f392fe255a7___backend__webgpu__webgpu_sys__gen_GpuError__GpuError___core_d941ad8722425d2e___result__Result_____wasm_bindgen_388cfc06d0305505___JsError___true_: (a: number, b: number, c: any) => [number, number];
    readonly wasm_bindgen_388cfc06d0305505___convert__closures_____invoke___wasm_bindgen_388cfc06d0305505___sys__JsNullable_wgpu_f5334f392fe255a7___backend__webgpu__webgpu_sys__gen_GpuError__GpuError___core_d941ad8722425d2e___result__Result_____wasm_bindgen_388cfc06d0305505___JsError___true__74: (a: number, b: number, c: any) => [number, number];
    readonly wasm_bindgen_388cfc06d0305505___convert__closures_____invoke___wasm_bindgen_388cfc06d0305505___sys__JsNullable_wgpu_f5334f392fe255a7___backend__webgpu__webgpu_sys__gen_GpuError__GpuError___core_d941ad8722425d2e___result__Result_____wasm_bindgen_388cfc06d0305505___JsError___true__75: (a: number, b: number, c: any) => [number, number];
    readonly wasm_bindgen_388cfc06d0305505___convert__closures_____invoke___wasm_bindgen_388cfc06d0305505___sys__JsNullable_wgpu_f5334f392fe255a7___backend__webgpu__webgpu_sys__gen_GpuError__GpuError___core_d941ad8722425d2e___result__Result_____wasm_bindgen_388cfc06d0305505___JsError___true__76: (a: number, b: number, c: any) => [number, number];
    readonly __wbindgen_malloc: (a: number, b: number) => number;
    readonly __wbindgen_realloc: (a: number, b: number, c: number, d: number) => number;
    readonly __wbindgen_exn_store: (a: number) => void;
    readonly __externref_table_alloc: () => number;
    readonly __wbindgen_externrefs: WebAssembly.Table;
    readonly __wbindgen_free: (a: number, b: number, c: number) => void;
    readonly __wbindgen_destroy_closure: (a: number, b: number) => void;
    readonly __externref_table_dealloc: (a: number) => void;
    readonly __externref_drop_slice: (a: number, b: number) => void;
    readonly __wbindgen_start: () => void;
}

export type SyncInitInput = BufferSource | WebAssembly.Module;

export function initSync(module: { module: SyncInitInput } | SyncInitInput): InitOutput;

export default function __wbg_init (module_or_path?: { module_or_path: InitInput | Promise<InitInput> } | InitInput | Promise<InitInput>): Promise<InitOutput>;
