// Compact instanced vertex shader for the dense cell layer.
//
// An instance is 16 bytes: a world position and a palette index. The colour
// comes from a ramp table uploaded once, and the cell size from a uniform,
// because a grid shares one size. That is a quarter of the per-instance
// bandwidth of the general sprite path.

struct Camera {
    viewProj: mat4x4<f32>,
    viewport: vec2<f32>,
};

struct Ramp {
    colors: array<vec4<f32>, 1024>,
};

struct Cell {
    size: vec2<f32>,
};

@group(0) @binding(0) var<uniform> camera: Camera;
@group(0) @binding(1) var<uniform> ramp:   Ramp;
@group(0) @binding(2) var<uniform> cell:   Cell;

struct VSIn {
    @location(0) pos: vec2<f32>,
    @location(1) color: vec4<f32>,
    @location(2) instancePos: vec2<f32>,
    @location(3) instancePalette: u32,
};

struct VSOut {
    @builtin(position) pos: vec4<f32>,
    @location(0) color: vec4<f32>,
    @location(1) uv: vec2<f32>,
};

@vertex
fn vs_main(in: VSIn) -> VSOut {
    var out: VSOut;

    // Palette zero draws nothing, so a cell can be cleared without being taken
    // out of the buffer. Collapsing the quad to a point is what discards it,
    // since the vertex stage cannot.
    if (in.instancePalette == 0u) {
        out.pos = vec4<f32>(0.0, 0.0, 2.0, 1.0);
        out.color = vec4<f32>(0.0);
        out.uv = vec2<f32>(0.0);
        return out;
    }

    let local = in.pos * cell.size + in.instancePos;
    out.pos = camera.viewProj * vec4<f32>(local, 0.0, 1.0);
    out.color = ramp.colors[(in.instancePalette - 1u) % 1024u] * in.color;
    out.uv = in.pos + vec2<f32>(0.5);
    return out;
}
