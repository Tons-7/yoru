/* tslint:disable */
/* eslint-disable */

/**
 * The device the engine runs on; one per page. `await FhEngine.create(options)`.
 */
export class FhEngine {
    private constructor();
    free(): void;
    [Symbol.dispose](): void;
    /**
     * Times the conv kernels of one configuration (5 to 15 s of GPU time; run it idle) and resolves with the table as
     * text for the `tune` session option; an empty string on a device without timestamp queries.
     */
    autotune(model: FhModel, options: any): Promise<any>;
    /**
     * The kernels compiled so far: record them (they are the same on every site) to `precompile` them next time.
     */
    compiledKernels(): string[];
    /**
     * Creates the WebGPU device (`{ precision: 'auto' | 'f16' | 'f32' }`; the device needs `shader-f16` for the f16
     * path and falls back to f32 without it).
     */
    static create(options: any): Promise<FhEngine>;
    /**
     * The `GPUDevice`, to present, sample and copy on the same device.
     */
    device(): any;
    /**
     * `{ name, class: 'tc-f16' | 'portable-f16' | 'portable-f32', tensor_cores, timestamps, bgra_storage }`
     */
    info(): any;
    /**
     * A model from its three files: `graph.json` text, the manifest text and the weight bytes.
     */
    loadModel(graph: string, manifest: string, weights: Uint8Array): FhModel;
    /**
     * Compiles the named kernels not compiled yet and returns how many; unknown names are skipped. Chrome compiles in
     * the process that composites the page and caches per site: a stream's worth at once freezes the video for about a
     * second on a new site. Pass a few names per animation frame (awaiting `device.queue.onSubmittedWorkDone()`
     * between) before `stream`, which then finds them compiled.
     */
    precompile(names: string[]): number;
    report(): any;
    /**
     * The plans of `model` for one frame size.
     */
    session(model: FhModel, options: any): FhSession;
    /**
     * A Stream of `model`: `session` are the session options, `stream` the stream options
     * (`{ mode: 'realtime' | 'video', cadence, sceneCut, heldFrames, inputs, outputs }`, camelCase or snake_case).
     */
    stream(model: FhModel, session: any, stream: any): FhStream;
}

export class FhModel {
    private constructor();
    free(): void;
    [Symbol.dispose](): void;
    /**
     * `{ name, train_step, graph_ops, tensors, render_sampling, format_version }`
     */
    info(): any;
}

/**
 * The scheduler between a video's frames and the display's ticks (ARCHITECTURE.md). Per
 * `requestAnimationFrame`: `tick(now)`, present what it says, generate its requests through the generator and
 * `generated(now, id, outputSlot)` each. Per video frame, once its kind is known: `sourceFrame(now, frame)`,
 * generate the requests it returns, then push the frame.
 */
export class FhPacer {
    free(): void;
    [Symbol.dispose](): void;
    /**
     * A measured GPU cost of `stage` ('pair' | 'mid' | 'present'). Returns whether it was taken (not inside the
     * settle window after a mode change).
     */
    costSample(now: number, stage: string, ms: number): boolean;
    delayMs(): number;
    factor(): number;
    /**
     * The host generated (submitted) the frame of request `request` into output slot `output`.
     */
    generated(now: number, request: number, output: number): void;
    hostStall(now: number, ms: number): void;
    /**
     * Fullscreen, resize: drops are exempt for the settle window.
     */
    modeChange(now: number): void;
    /**
     * `options`: any subset of the Pacer options (`{ mode: 'auto' | 'display_hz' | { factor: 3 } | { target: 120 },
     * fpsLimit, minPresentIntervalMs, ... }`, camelCase or snake_case); unknown keys are rejected with the valid list.
     */
    constructor(options: any);
    options(): any;
    penalty(): number;
    /**
     * Records every call from now on into a `trace v1` object named after `host` (a user agent, a page), with the
     * options in force: what a bug report attaches, and what `fh_pacer_replay` and `flyhigh-dev replay` reproduce.
     */
    record(host: string): void;
    report(): any;
    /**
     * Seek, new source.
     */
    reset(now: number): void;
    /**
     * Changes options live; a new mode restarts the controller and the output grid.
     */
    setOptions(now: number, options: any): void;
    /**
     * The generator answered that the source frame is enough for the request.
     */
    skipped(request: number): void;
    /**
     * `frame`: `{ id, ptsMs, kind: 'motion' | 'cut' | 'hold', motion, skipped }`. Returns the requests to generate
     * before the pair changes.
     */
    sourceFrame(now: number, frame: any): any;
    /**
     * Ends the recording and returns it (`null` when there was none).
     */
    stopRecording(): any;
    /**
     * `{ present: { frame: { source: id } | { output: id }, due_ms, late_ms, mid, fallback } | null,
     *    generate: [{ id, previous, current, t, due_ms, retime, probe }], factor, dropped }`
     */
    tick(now: number): any;
    /**
     * The recording so far as a plain object, or `null` when not recording.
     */
    trace(): any;
}

/**
 * Two frames in, one frame at phase `t` out, on textures the session owns.
 */
export class FhSession {
    private constructor();
    free(): void;
    [Symbol.dispose](): void;
    addInput(): number;
    addOutput(): number;
    generate(a: number, b: number, t: number, out: number): void;
    /**
     * The slot's `GPUTexture` (rgba8unorm).
     */
    inputTexture(slot: number): any;
    /**
     * Runs one more generated frame of the last pair and resolves with its GPU milliseconds (`null` without timestamp
     * queries).
     */
    midTimed(t: number, out: number): Promise<any>;
    /**
     * Phase `t` of the last pair into output slot `out`.
     */
    mid(t: number, out: number): void;
    outputTexture(slot: number): any;
    /**
     * Runs the pair stage of (`a`, `b`) and resolves with its GPU milliseconds (timestamp queries; `null` without
     * them).
     */
    pairTimed(a: number, b: number): Promise<any>;
    /**
     * The motion of the interval (`a`, `b`).
     */
    pair(a: number, b: number): void;
    pushBitmap(slot: number, b: ImageBitmap): void;
    pushCanvas(slot: number, c: HTMLCanvasElement): void;
    /**
     * Copies a frame into an input slot (sizes must match). Firefox rejects `<video>` sources: blit through
     * `importExternalTexture` into `inputTexture(slot)` there.
     */
    pushVideo(slot: number, v: HTMLVideoElement): void;
    /**
     * Resolves with the output slot's RGBA8 bytes (a `Uint8Array`).
     */
    readRgba(out: number): Promise<any>;
    report(): any;
    /**
     * Compiles every kernel the session runs (one pair and one mid); await `device.queue.onSubmittedWorkDone()` after
     * it, before the first real frame.
     */
    warmUp(): void;
    /**
     * The protection mask (`protectionMask`): width * height bytes, 0 interpolates, anything else is taken from the
     * source frames; applies from the next pair.
     */
    writeMask(mask: Uint8Array): void;
    /**
     * RGBA8 bytes into an input slot.
     */
    writeRgba(slot: number, rgba: Uint8Array): void;
}

/**
 * Frames with timestamps in, intervals out (ARCHITECTURE.md); the Pacer decides the phases and their times.
 */
export class FhStream {
    private constructor();
    free(): void;
    [Symbol.dispose](): void;
    addInput(): number;
    addOutput(): number;
    /**
     * Classifies the pair (`previous`, `slot`) before pushing it: resolves with `{ kind: 'motion' | 'cut' | 'hold',
     * mean, max, black, motion }`, or `{ kind: 'motion', busy: true }` when every readback is still in flight. In
     * Firefox submit an empty command buffer every millisecond while it is pending (`device.queue.submit([])`).
     */
    classify(previous: number, slot: number): Promise<any>;
    /**
     * `generate` with its GPU time: `{ written, cost }`, `cost` a promise of milliseconds (`null` without timestamp
     * queries, when nothing was written, or for a cadence composite). The Pacer's `costSample(now, 'mid', ms)` takes it; its
     * probe requests are meant for this call.
     */
    generateTimed(t: number, out: number): any;
    /**
     * Phase `t` of the current interval into output slot `out`; false = nothing written (t = 0 without re-timing:
     * the source frame is enough).
     */
    generate(t: number, out: number): boolean;
    inputTexture(slot: number): any;
    /**
     * Mouse counts at time `t` for the camera warp (`movementX` / `movementY` of a pointer-locked page): `t` on the
     * clock of `push`'s `pts`, which must then be the frames' arrival times (`performance.now()`), not media time.
     * The first call starts the warp.
     */
    mouse(t: number, dx: number, dy: number): void;
    /**
     * The slot to write the next source frame into: never one the stream still reads nor one in `inUse` (an array
     * of slots the host still shows); -1 when the ring is exhausted (add inputs).
     */
    nextInput(in_use: any): number;
    options(): any;
    outputTexture(slot: number): any;
    pushBitmap(slot: number, b: ImageBitmap): void;
    pushCanvas(slot: number, c: HTMLCanvasElement): void;
    /**
     * `push` with the pair's GPU time: the same object plus `cost`, a promise of milliseconds (`null` without
     * timestamp queries, or when no pair ran). The Pacer's `costSample(now, 'pair', ms)` takes it.
     */
    pushTimed(slot: number, pts: number): any;
    /**
     * Copies a frame into an input slot. Firefox rejects `<video>` sources: blit through `importExternalTexture`
     * into `inputTexture(slot)` there.
     */
    pushVideo(slot: number, v: HTMLVideoElement): void;
    /**
     * The newest source frame is in `slot` with media timestamp `pts` (ms): runs its pair. Returns
     * `{ ready, baseSlot, statsPending, startPts, endPts }`.
     */
    push(slot: number, pts: number): any;
    /**
     * Resolves with an output slot's RGBA8 bytes (tests; a player presents the texture).
     */
    readRgba(out: number): Promise<any>;
    /**
     * Whether the current interval's real slot is re-timed (present `generate(0, out)` instead of the source).
     */
    realReplaced(): boolean;
    report(): any;
    /**
     * Forget the pushed frames after a seek or a decoder gap.
     */
    reset(): void;
    /**
     * Whether the pointer is locked (a game camera) or free (a cursor): free holds the camera warp off at once.
     */
    setMouseCaptured(captured: boolean): void;
    /**
     * Video + cadence: resolves when the pending pair statistics are in (await it after a push with `statsPending`,
     * before the interval's first `generate`).
     */
    stats(): Promise<any>;
    /**
     * Compiles every kernel the stream runs (the warm-up pushes and one mid on its own slots, then `reset`); await
     * `device.queue.onSubmittedWorkDone()` after it, before the first real frame.
     */
    warmUp(): void;
    /**
     * The frame about to be presented at `now` (input slot `slot`, or output slot `slot` when `output`) turned by the
     * mouse motion since the moment it shows, into output slot `out`. True: present `out`; false: the warp is off,
     * present the frame as it is.
     */
    warp(output: boolean, slot: number, out: number, now: number): boolean;
    /**
     * The protection mask (`protectionMask` of the session options): width * height bytes, 0 interpolates, anything
     * else is taken from the source frames; applies from the next pair.
     */
    writeMask(mask: Uint8Array): void;
}

/**
 * Replays a `trace v1` object through a fresh Pacer: the report and the per-second table, as `flyhigh-dev replay
 * --json` prints them. Options: the trace's own when `options` is null.
 */
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
    readonly wasm_bindgen_42694e960ec4e618___convert__closures_____invoke___js_sys_f59ae7d0bedcb30e___Function_fn_wasm_bindgen_42694e960ec4e618___JsValue_____wasm_bindgen_42694e960ec4e618___sys__Undefined___js_sys_f59ae7d0bedcb30e___Function_fn_wasm_bindgen_42694e960ec4e618___JsValue_____wasm_bindgen_42694e960ec4e618___sys__Undefined_______true_: (a: number, b: number, c: any, d: any) => void;
    readonly wasm_bindgen_42694e960ec4e618___convert__closures_____invoke___wasm_bindgen_42694e960ec4e618___sys__JsNullable_wgpu_9dd11b04e7018978___backend__webgpu__webgpu_sys__gen_GpuAdapter__GpuAdapter___core_37ee0ce0c4cd57d5___result__Result_____wasm_bindgen_42694e960ec4e618___JsError___true_: (a: number, b: number, c: any) => [number, number];
    readonly wasm_bindgen_42694e960ec4e618___convert__closures_____invoke___wasm_bindgen_42694e960ec4e618___sys__JsNullable_wgpu_9dd11b04e7018978___backend__webgpu__webgpu_sys__gen_GpuAdapter__GpuAdapter___core_37ee0ce0c4cd57d5___result__Result_____wasm_bindgen_42694e960ec4e618___JsError___true__73: (a: number, b: number, c: any) => [number, number];
    readonly wasm_bindgen_42694e960ec4e618___convert__closures_____invoke___wasm_bindgen_42694e960ec4e618___sys__JsNullable_wgpu_9dd11b04e7018978___backend__webgpu__webgpu_sys__gen_GpuAdapter__GpuAdapter___core_37ee0ce0c4cd57d5___result__Result_____wasm_bindgen_42694e960ec4e618___JsError___true__74: (a: number, b: number, c: any) => [number, number];
    readonly wasm_bindgen_42694e960ec4e618___convert__closures_____invoke___wasm_bindgen_42694e960ec4e618___sys__JsNullable_wgpu_9dd11b04e7018978___backend__webgpu__webgpu_sys__gen_GpuAdapter__GpuAdapter___core_37ee0ce0c4cd57d5___result__Result_____wasm_bindgen_42694e960ec4e618___JsError___true__75: (a: number, b: number, c: any) => [number, number];
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

/**
 * Instantiates the given `module`, which can either be bytes or
 * a precompiled `WebAssembly.Module`.
 *
 * @param {{ module: SyncInitInput }} module - Passing `SyncInitInput` directly is deprecated.
 *
 * @returns {InitOutput}
 */
export function initSync(module: { module: SyncInitInput } | SyncInitInput): InitOutput;

/**
 * If `module_or_path` is {RequestInfo} or {URL}, makes a request and
 * for everything else, calls `WebAssembly.instantiate` directly.
 *
 * @param {{ module_or_path: InitInput | Promise<InitInput> }} module_or_path - Passing `InitInput` directly is deprecated.
 *
 * @returns {Promise<InitOutput>}
 */
export default function __wbg_init (module_or_path?: { module_or_path: InitInput | Promise<InitInput> } | InitInput | Promise<InitInput>): Promise<InitOutput>;
