struct Camera {
    view_proj : mat4x4<f32>,
};

@group(0) @binding(0) var<uniform> cam : Camera;

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
    out.pos = cam.view_proj * vec4<f32>(f32(in.pos.x), f32(in.pos.y), 0.0, 1.0);
    out.color = in.color;
    return out;
}

@fragment
fn fs_main(in : VSOut) -> @location(0) vec4<f32> {
    return in.color;
}
