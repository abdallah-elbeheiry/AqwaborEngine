// GPU compute cull shader.
// Reads all instances, tests visibility against the camera frustum,
// compacts surviving instances into an output buffer, and writes
// the draw count into an indirect command buffer.

struct InstanceData {
    position: vec2<f32>,
    scale:    vec2<f32>,
    rotation: f32,
    color:    vec4<f32>,
    uvOffset: vec2<f32>,
    layer:    f32,
    pad:      f32, // trailing pad: WGSL storage stride is 64 bytes, must match Go
};

struct Camera {
    viewProj: mat4x4<f32>,
    viewport: vec2<f32>,
};

struct CullParams {
    instanceCount: u32,
    // outputBase is the first index of this cull's region in the output buffer.
    // Several culls share one buffer, each compacting from its own base, so a
    // second cull in a frame no longer overwrites the first.
    outputBase:    u32,
    minBounds:     vec2<f32>,
    maxBounds:     vec2<f32>,
};

// Indirect command buffer layout matches WebGPU DrawIndexedIndirectCommand.
struct IndirectCmd {
    indexCount:    u32,
    instanceCount: atomic<u32>,
    firstIndex:    u32,
    baseVertex:    i32,
    firstInstance: u32,
};

@group(0) @binding(0) var<storage, read>       inputInstances:  array<InstanceData>;
@group(0) @binding(1) var<storage, read_write>  outputInstances: array<InstanceData>;
@group(0) @binding(2) var<storage, read_write>  indirectCmd:     IndirectCmd;
@group(0) @binding(3) var<uniform>              camera:          Camera;
@group(0) @binding(4) var<uniform>              cullParams:      CullParams;

fn isCulled(pos: vec2<f32>, scale: vec2<f32>) -> bool {
    let halfW = scale.x * 0.5;
    let halfH = scale.y * 0.5;

    // World-bounds cull
    if (cullParams.minBounds.x < cullParams.maxBounds.x) {
        if (pos.x + halfW < cullParams.minBounds.x ||
            pos.x - halfW > cullParams.maxBounds.x ||
            pos.y + halfH < cullParams.minBounds.y ||
            pos.y - halfH > cullParams.maxBounds.y) {
            return true;
        }
    }

    // Clip-space frustum test: transform AABB corners individually.
    // No local arrays — naga emits malformed MSL for dynamically-indexed
    // local arrays, so each corner is a separate let binding.
    let c0 = camera.viewProj * vec4<f32>(pos + vec2<f32>(-halfW, -halfH), 0.0, 1.0);
    let c1 = camera.viewProj * vec4<f32>(pos + vec2<f32>( halfW, -halfH), 0.0, 1.0);
    let c2 = camera.viewProj * vec4<f32>(pos + vec2<f32>( halfW,  halfH), 0.0, 1.0);
    let c3 = camera.viewProj * vec4<f32>(pos + vec2<f32>(-halfW,  halfH), 0.0, 1.0);

    // Behind camera (all corners have w <= 0)
    if (c0.w <= 0.0 && c1.w <= 0.0 && c2.w <= 0.0 && c3.w <= 0.0) {
        return true;
    }

    var allLeft = true;
    var allRight = true;
    var allBelow = true;
    var allAbove = true;

    if (c0.w > 0.0) {
        let nx = c0.x / c0.w;
        let ny = c0.y / c0.w;
        if (nx > -1.0) { allLeft = false; }
        if (nx <  1.0) { allRight = false; }
        if (ny > -1.0) { allBelow = false; }
        if (ny <  1.0) { allAbove = false; }
    }
    if (c1.w > 0.0) {
        let nx = c1.x / c1.w;
        let ny = c1.y / c1.w;
        if (nx > -1.0) { allLeft = false; }
        if (nx <  1.0) { allRight = false; }
        if (ny > -1.0) { allBelow = false; }
        if (ny <  1.0) { allAbove = false; }
    }
    if (c2.w > 0.0) {
        let nx = c2.x / c2.w;
        let ny = c2.y / c2.w;
        if (nx > -1.0) { allLeft = false; }
        if (nx <  1.0) { allRight = false; }
        if (ny > -1.0) { allBelow = false; }
        if (ny <  1.0) { allAbove = false; }
    }
    if (c3.w > 0.0) {
        let nx = c3.x / c3.w;
        let ny = c3.y / c3.w;
        if (nx > -1.0) { allLeft = false; }
        if (nx <  1.0) { allRight = false; }
        if (ny > -1.0) { allBelow = false; }
        if (ny <  1.0) { allAbove = false; }
    }

    if (allLeft || allRight || allBelow || allAbove) {
        return true;
    }
    return false;
}

@compute @workgroup_size(64)
fn main(@builtin(global_invocation_id) id: vec3<u32>) {
    let i = id.x;
    if (i >= cullParams.instanceCount) { return; }

    let inst = inputInstances[i];

    // Skip inactive (zero scale)
    if (inst.scale.x <= 0.0 && inst.scale.y <= 0.0) {
        return;
    }

    if (isCulled(inst.position, inst.scale)) {
        return;
    }

    // Compact: atomically claim a place in this cull's region and write there.
    let idx = atomicAdd(&indirectCmd.instanceCount, 1u);
    outputInstances[cullParams.outputBase + idx] = inst;
}
