// Stroke vertex shader: expands segment instances into screen-space quads.
//
// Each instance is one line segment with adjacency data for miter joins.
// The 4 vertices of a unit quad (slot 0) are offset by ±normal*halfWidth
// along the segment, producing a screen-space-width polyline.
//
// Bindings:
//   group(0) binding(0) = Camera uniform (viewProj + viewport)

struct Camera {
    viewProj: mat4x4<f32>,
    viewport: vec2<f32>,
};

@group(0) @binding(0) var<uniform> camera: Camera;

// Segment instance (slot 1).  Must match Go StrokeSegment (72 bytes).
struct Segment {
    p0:     vec2<f32>,
    p1:     vec2<f32>,
    prev:   vec2<f32>,
    next:   vec2<f32>,
    color0: vec4<f32>,
    color1: vec4<f32>,
    width:  f32,
    flags:  u32,   // low 8 bits: 0=pixels, 1=world
    layer:  u32,
    _pad:   vec2<u32>,
};

struct VSOut {
    @builtin(position) pos: vec4<f32>,
    @location(0) color: vec4<f32>,
    @location(1) uv:    vec2<f32>,
};

// Compute screen-space position and direction between two world points.
// Returns (screenPos, screenDir) where screenDir is a unit vector in pixels.
fn toScreen(world: vec2<f32>) -> vec2<f32> {
    let clip = camera.viewProj * vec4<f32>(world, 0.0, 1.0);
    // Viewport transform: NDC xy -> pixels
    return vec2<f32>(
        (clip.x / clip.w * 0.5 + 0.5) * camera.viewport.x,
        (1.0 - (clip.y / clip.w * 0.5 + 0.5)) * camera.viewport.y,
    );
}

fn screenDir(a: vec2<f32>, b: vec2<f32>) -> vec2<f32> {
    let sa = toScreen(a);
    let sb = toScreen(b);
    let d = sb - sa;
    let len = length(d);
    if (len < 0.001) {
        return vec2<f32>(1.0, 0.0);
    }
    return d / len;
}

fn screenNormal(a: vec2<f32>, b: vec2<f32>) -> vec2<f32> {
    let d = screenDir(a, b);
    return vec2<f32>(-d.y, d.x);
}

@vertex
fn vs_main(
    @location(8) quadVert:  vec2<f32>,
    @location(0) segP0:     vec2<f32>,
    @location(1) segP1:     vec2<f32>,
    @location(2) segPrev:   vec2<f32>,
    @location(3) segNext:   vec2<f32>,
    @location(4) segColor0: vec4<f32>,
    @location(5) segColor1: vec4<f32>,
    @location(6) segWidth:  f32,
    @location(7) segFlags:  vec2<u32>,
) -> VSOut {
    var out: VSOut;

    // Screen-space normals for the two ends of the segment.
    // Use prev→p0 and p1→next for the adjacent directions.
    let n0 = screenNormal(segPrev, segP0);
    let n1 = screenNormal(segP1, segNext);
    // Segment normal (for straight segments, n0 == n1).
    let ns = screenNormal(segP0, segP1);

    // For miter: average the two endpoint normals.
    // At endpoints (where prev or next duplicate the point), the normals
    // degenerate to the segment normal, giving a clean butt cap.
    let nMiter0 = normalize(n0 + ns);
    let nMiter1 = normalize(ns + n1);

    // Miter length correction: clamp miter spike to 2x width.
    let miterLen0 = min(1.0 / max(dot(nMiter0, ns), 0.01), 2.0);
    let miterLen1 = min(1.0 / max(dot(nMiter1, ns), 0.01), 2.0);

    let halfW = segWidth * 0.5;

    // quadVert.y: -1 = start end, +1 = end end
    // quadVert.x: -1 = left side, +1 = right side
    let side = quadVert.x;
    let endT = (quadVert.y + 1.0) * 0.5; // 0 at start, 1 at end

    // Interpolate position along the segment.
    let centerWorld = mix(segP0, segP1, endT);
    let centerClip = camera.viewProj * vec4<f32>(centerWorld, 0.0, 1.0);

    // Interpolate normal and miter length.
    let n = mix(nMiter0, nMiter1, endT);
    let miterL = mix(miterLen0, miterLen1, endT);

    // Offset in screen space, then project back.
    let offset = side * n * halfW * miterL;
    // Convert pixel offset to clip space (approximate: divide by viewport half-size).
    let clipOffset = vec2<f32>(
        offset.x / (camera.viewport.x * 0.5),
        -offset.y / (camera.viewport.y * 0.5),
    );

    out.pos = vec4<f32>(
        centerClip.xy + clipOffset * centerClip.w,
        centerClip.zw,
    );

    // Interpolate color along the segment.
    out.color = mix(segColor0, segColor1, endT);
    out.uv = vec2<f32>(side * 0.5 + 0.5, endT);

    return out;
}
