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
    _pad:          u32,
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

    // Clip-space frustum test: transform AABB corners
    let corners = array<vec4<f32>, 4>(
        camera.viewProj * vec4<f32>(pos + vec2<f32>(-halfW, -halfH), 0.0, 1.0),
        camera.viewProj * vec4<f32>(pos + vec2<f32>( halfW, -halfH), 0.0, 1.0),
        camera.viewProj * vec4<f32>(pos + vec2<f32>( halfW,  halfH), 0.0, 1.0),
        camera.viewProj * vec4<f32>(pos + vec2<f32>(-halfW,  halfH), 0.0, 1.0),
    );

    // Behind camera (all corners have w <= 0)
    var allBehind = true;
    for (var i = 0u; i < 4u; i++) {
        if (corners[i].w > 0.0) {
            allBehind = false;
            break;
        }
    }
    if (allBehind) { return true; }

    // Left/right frustum
    var allLeft = true;
    var allRight = true;
    for (var i = 0u; i < 4u; i++) {
        if (corners[i].w > 0.0) {
            let nx = corners[i].x / corners[i].w;
            if (nx > -1.0) { allLeft = false; }
            if (nx <  1.0) { allRight = false; }
        }
    }
    if (allLeft || allRight) { return true; }

    // Top/bottom frustum
    var allBelow = true;
    var allAbove = true;
    for (var i = 0u; i < 4u; i++) {
        if (corners[i].w > 0.0) {
            let ny = corners[i].y / corners[i].w;
            if (ny > -1.0) { allBelow = false; }
            if (ny <  1.0) { allAbove = false; }
        }
    }
    if (allBelow || allAbove) { return true; }

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

    // Compact: atomically claim an output slot and write
    let idx = atomicAdd(&indirectCmd.instanceCount, 1u);
    outputInstances[idx] = inst;
}
