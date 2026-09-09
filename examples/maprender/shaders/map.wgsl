struct Camera {
    viewProj : mat4x4<f32>,
    viewport : vec2<f32>,
};

@group(0) @binding(0) var<uniform> camera : Camera;

struct VSIn {
    @location(0) pos : vec2<i32>,
    @location(1) color : vec4<f32>,
};

struct VSOut {
    @builtin(position) pos : vec4<f32>,
    @location(0) color : vec4<f32>,
};

@vertex
fn vs_main(in : VSIn) -> VSOut {
    var out : VSOut;
    out.pos = camera.viewProj * vec4<f32>(f32(in.pos.x), f32(in.pos.y), 0.0, 1.0);
    out.color = in.color;
    return out;
}
