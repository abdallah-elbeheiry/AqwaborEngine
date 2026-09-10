// Instanced vertex shader for the unified render pipeline.
// Locations 0-1: mesh vertex data (pos, color)
// Locations 2-7: per-instance data (pos, scale, rot, color, uv, layer)
// Binding group 0: camera uniform

struct Camera {
    viewProj: mat4x4<f32>,
    viewport: vec2<f32>,
};

@group(0) @binding(0)
var<uniform> camera: Camera;

struct VSIn {
    // mesh vertex (slot 0)
    @location(0) pos: vec2<f32>,
    @location(1) color: vec4<f32>,
    // instance data (slot 1)
    @location(2) instancePos: vec2<f32>,
    @location(3) instanceScale: vec2<f32>,
    @location(4) instanceRot: f32,
    @location(5) instanceColor: vec4<f32>,
    @location(6) instanceUVOffset: vec2<f32>,
    @location(7) instanceLayer: f32,
};

struct VSOut {
    @builtin(position) pos: vec4<f32>,
    @location(0) color: vec4<f32>,
    @location(1) uv: vec2<f32>,
};

fn rotate2d(p: vec2<f32>, angle: f32) -> vec2<f32> {
    let s = sin(angle);
    let c = cos(angle);
    return vec2<f32>(
        p.x * c - p.y * s,
        p.x * s + p.y * c
    );
}

@vertex
fn vs_main(in: VSIn) -> VSOut {
    var out: VSOut;

    // Scale, rotate, translate the mesh vertex by the instance transform
    var local = in.pos * in.instanceScale;
    local = rotate2d(local, in.instanceRot);
    local = local + in.instancePos;

    // Project through camera
    out.pos = camera.viewProj * vec4<f32>(local, 0.0, 1.0);
    out.color = in.instanceColor * in.color;
    out.uv = in.pos + vec2<f32>(0.5) + in.instanceUVOffset;

    return out;
}
