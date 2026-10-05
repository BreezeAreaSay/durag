// Custom fragment shaders (PixiJS v8 filters, WebGL / GLSL ES 3.0):
//   - halftone: risograph dot screen in the darker tones
//   - chromatic: cheap misregistered-print RGB split
//   - hologram: pointer-driven RGB gradient for the Super card
import { Filter, GlProgram } from 'pixi.js';

// PixiJS v8's default filter vertex shader.
const VERTEX = `precision highp float;
in vec2 aPosition;
out vec2 vTextureCoord;

uniform vec4 uInputSize;
uniform vec4 uOutputFrame;
uniform vec4 uOutputTexture;

vec4 filterVertexPosition(void) {
  vec2 position = aPosition * uOutputFrame.zw + uOutputFrame.xy;
  position.x = position.x * (2.0 / uOutputTexture.x) - 1.0;
  position.y = position.y * (2.0 * uOutputTexture.z / uOutputTexture.y) - uOutputTexture.z;
  return vec4(position, 0.0, 1.0);
}

vec2 filterTextureCoord(void) {
  return aPosition * (uOutputFrame.zw * uInputSize.zw);
}

void main(void) {
  gl_Position = filterVertexPosition();
  vTextureCoord = filterTextureCoord();
}
`;

const HALFTONE_FRAG = `precision highp float;
in vec2 vTextureCoord;
out vec4 finalColor;

uniform sampler2D uTexture;
uniform highp vec4 uInputSize;
uniform float uDotSize;
uniform float uAngle;
uniform float uStrength;

void main(void) {
  vec4 color = texture(uTexture, vTextureCoord);
  if (color.a < 0.01) { finalColor = color; return; }
  vec3 straight = color.rgb / color.a;
  float lum = dot(straight, vec3(0.299, 0.587, 0.114));

  vec2 px = vTextureCoord * uInputSize.xy;
  float s = sin(uAngle);
  float c = cos(uAngle);
  vec2 rot = vec2(c * px.x - s * px.y, s * px.x + c * px.y);
  vec2 cell = mod(rot, uDotSize) - uDotSize * 0.5;
  float radius = (1.0 - lum) * uDotSize * 0.55;
  float d = length(cell);
  float dotMask = 1.0 - smoothstep(radius - 0.8, radius + 0.2, d);
  // only mid-tones and shadows get the screen; paper stays clean
  float tone = smoothstep(0.98, 0.6, lum);
  vec3 ink = vec3(0.06, 0.05, 0.05);
  vec3 result = mix(straight, ink, dotMask * tone * uStrength);
  finalColor = vec4(result * color.a, color.a);
}
`;

const CHROMATIC_FRAG = `precision highp float;
in vec2 vTextureCoord;
out vec4 finalColor;

uniform sampler2D uTexture;
uniform highp vec4 uInputSize;
uniform highp vec4 uInputClamp;
uniform float uOffset;

void main(void) {
  vec2 d = vec2(uOffset, 0.0) * uInputSize.zw;
  vec4 c = texture(uTexture, vTextureCoord);
  float r = texture(uTexture, clamp(vTextureCoord + d, uInputClamp.xy, uInputClamp.zw)).r;
  float b = texture(uTexture, clamp(vTextureCoord - d, uInputClamp.xy, uInputClamp.zw)).b;
  finalColor = vec4(r, c.g, b, c.a);
}
`;

const HOLOGRAM_FRAG = `precision highp float;
in vec2 vTextureCoord;
out vec4 finalColor;

uniform sampler2D uTexture;
uniform highp vec4 uInputSize;
uniform vec2 uPointer;
uniform float uTime;
uniform float uStrength;

vec3 hsv2rgb(vec3 c) {
  vec4 K = vec4(1.0, 2.0 / 3.0, 1.0 / 3.0, 3.0);
  vec3 p = abs(fract(c.xxx + K.xyz) * 6.0 - K.www);
  return c.z * mix(K.xxx, clamp(p - K.xxx, 0.0, 1.0), c.y);
}

void main(void) {
  vec4 color = texture(uTexture, vTextureCoord);
  if (color.a < 0.01) { finalColor = color; return; }
  vec3 straight = color.rgb / color.a;
  vec2 uv = vTextureCoord * uInputSize.xy / uInputSize.y;
  vec2 p = uPointer - vec2(0.5);
  float angle = atan(p.y, p.x);
  float hue = fract(uv.x * 1.4 + uv.y * 0.9 + angle * 0.35 + length(p) * 1.2 + uTime * 0.08);
  float bands = 0.5 + 0.5 * sin((uv.x + uv.y) * 40.0 - uTime * 2.0 + angle * 3.0);
  vec3 rainbow = hsv2rgb(vec3(hue, 0.75, 1.0)) * (0.8 + 0.2 * bands);
  float lum = dot(straight, vec3(0.299, 0.587, 0.114));
  float mask = smoothstep(0.45, 0.95, lum); // only light areas shimmer
  vec3 result = mix(straight, rainbow, mask * uStrength);
  finalColor = vec4(result * color.a, color.a);
}
`;

export function createHalftoneFilter(opts: { dotSize?: number; angle?: number; strength?: number } = {}): Filter {
  return new Filter({
    glProgram: GlProgram.from({ vertex: VERTEX, fragment: HALFTONE_FRAG, name: 'durag-halftone' }),
    resources: {
      halftoneUniforms: {
        uDotSize: { value: opts.dotSize ?? 5, type: 'f32' },
        uAngle: { value: opts.angle ?? 0.45, type: 'f32' },
        uStrength: { value: opts.strength ?? 0.55, type: 'f32' },
      },
    },
  });
}

export function createChromaticFilter(offset = 1.2): Filter {
  return new Filter({
    glProgram: GlProgram.from({ vertex: VERTEX, fragment: CHROMATIC_FRAG, name: 'durag-chromatic' }),
    resources: {
      chromaticUniforms: {
        uOffset: { value: offset, type: 'f32' },
      },
    },
    padding: 4,
  });
}

export interface HologramFilter extends Filter {
  setPointer(x: number, y: number): void;
  setTime(seconds: number): void;
}

export function createHologramFilter(strength = 0.65): HologramFilter {
  const filter = new Filter({
    glProgram: GlProgram.from({ vertex: VERTEX, fragment: HOLOGRAM_FRAG, name: 'durag-hologram' }),
    resources: {
      hologramUniforms: {
        uPointer: { value: new Float32Array([0.5, 0.5]), type: 'vec2<f32>' },
        uTime: { value: 0, type: 'f32' },
        uStrength: { value: strength, type: 'f32' },
      },
    },
  }) as HologramFilter;
  const uniforms = (filter.resources as { hologramUniforms: { uniforms: { uPointer: Float32Array; uTime: number } } }).hologramUniforms.uniforms;
  filter.setPointer = (x, y) => {
    uniforms.uPointer[0] = x;
    uniforms.uPointer[1] = y;
  };
  filter.setTime = (seconds) => {
    uniforms.uTime = seconds;
  };
  return filter;
}

/**
 * Compiles the filter's program on the given renderer. PixiJS compiles lazily
 * during the first render and only logs failures, which would leave the whole
 * filtered container invisible; validating up front lets the scene fall back
 * to rendering without effects on exotic GPUs.
 */
export function filterCompiles(renderer: unknown, filter: Filter): boolean {
  try {
    const shaderSystem = (renderer as { shader?: { _getProgramData?: (p: unknown) => unknown; getProgramData?: (p: unknown) => unknown } }).shader;
    const get = shaderSystem?._getProgramData ?? shaderSystem?.getProgramData;
    if (!shaderSystem || !get) return true; // unknown renderer internals: let PixiJS decide
    get.call(shaderSystem, filter.glProgram);
    return true;
  } catch (err) {
    console.warn('durag: shader failed to compile, effects disabled', err);
    return false;
  }
}
