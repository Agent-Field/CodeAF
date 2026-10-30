// Machine names, ported from internal/relay/name.go: a name is derived from the machine's own key, so the
// relay can refuse a registration under a name that key does not derive. test/engine.test.js holds the port
// to vectors the Go side computes.

// NAME_SALT is an on-the-wire identifier, not product prose.
const NAME_SALT = new TextEncoder().encode('aforge relay name\0'); // legacy-name

const WORDS = [
  'acorn', 'alder', 'amber', 'anchor', 'apple', 'arbor', 'arrow', 'ash',
  'aspen', 'atlas', 'aurora', 'autumn', 'azure', 'badger', 'bamboo', 'banjo',
  'basil', 'beacon', 'beetle', 'birch', 'bishop', 'bison', 'blossom', 'bluff',
  'bobcat', 'bonfire', 'boulder', 'bramble', 'brass', 'breeze', 'bridge', 'bronze',
  'brook', 'buffalo', 'burrow', 'cabin', 'cactus', 'camel', 'canary', 'candle',
  'canoe', 'canyon', 'cardinal', 'carrot', 'cascade', 'cedar', 'cello', 'chalk',
  'cherry', 'chestnut', 'chimney', 'cinder', 'citrus', 'clover', 'cobalt', 'comet',
  'compass', 'copper', 'coral', 'cottage', 'cougar', 'crane', 'crater', 'cricket',
  'crimson', 'crocus', 'cypress', 'daffodil', 'dahlia', 'daisy', 'dapple', 'dawn',
  'delta', 'denim', 'dingo', 'dolphin', 'domino', 'donkey', 'dragon', 'driftwood',
  'dune', 'eagle', 'ember', 'emerald', 'falcon', 'fennel', 'fern', 'ferry',
  'fiddle', 'finch', 'flint', 'flute', 'forest', 'fossil', 'fountain', 'foxglove',
  'gadget', 'gallery', 'garnet', 'gazelle', 'geode', 'geyser', 'ginger', 'glacier',
  'glider', 'granite', 'grotto', 'guitar', 'gulch', 'gypsum', 'harbor', 'harvest',
  'hazel', 'heather', 'hedge', 'heron', 'hickory', 'hollow', 'honey', 'hornet',
  'ibis', 'iceberg', 'indigo', 'iris', 'island', 'ivory', 'jackal', 'jasmine',
  'jasper', 'jetty', 'jigsaw', 'juniper', 'kayak', 'kelp', 'kestrel', 'kettle',
  'kingfisher', 'kite', 'koala', 'lagoon', 'lamp', 'lantern', 'larch', 'lattice',
  'lavender', 'ledger', 'lemon', 'lichen', 'lighthouse', 'lilac', 'linen', 'lobster',
  'lotus', 'lumber', 'lupine', 'lynx', 'magnet', 'magnolia', 'mahogany', 'mallard',
  'mango', 'maple', 'marble', 'marigold', 'marlin', 'marsh', 'meadow', 'mesa',
  'meteor', 'mica', 'midnight', 'mimosa', 'mineral', 'mint', 'mirror', 'mist',
  'mongoose', 'moonlight', 'moraine', 'mosaic', 'moss', 'mulberry', 'mustang', 'nectar',
  'nettle', 'nimbus', 'nomad', 'nutmeg', 'oasis', 'obsidian', 'ocelot', 'octopus',
  'olive', 'onyx', 'opal', 'orchard', 'orchid', 'oriole', 'osprey', 'otter',
  'paddle', 'pampas', 'pansy', 'papaya', 'parsley', 'pebble', 'pelican', 'pepper',
  'periwinkle', 'petal', 'pewter', 'phoenix', 'piano', 'pigeon', 'pilot', 'pine',
  'pistachio', 'plateau', 'plover', 'plum', 'pollen', 'pomelo', 'poppy', 'porcelain',
  'prairie', 'puffin', 'pumice', 'quail', 'quartz', 'quiver', 'rabbit', 'raccoon',
  'radish', 'rafter', 'rapids', 'raven', 'redwood', 'reef', 'rhubarb', 'ribbon',
  'river', 'robin', 'rosemary', 'rudder', 'saffron', 'sage', 'salmon', 'sandal',
  'sapphire', 'sardine', 'satchel', 'scarlet', 'seagull', 'sequoia', 'shale', 'willow',
];

/** nameFor is the name a machine's 32-byte X25519 public key derives. */
export async function nameFor(publicKey) {
  const salted = new Uint8Array(NAME_SALT.length + publicKey.length);
  salted.set(NAME_SALT);
  salted.set(publicKey, NAME_SALT.length);
  const sum = new Uint8Array(await crypto.subtle.digest('SHA-256', salted));
  return `${WORDS[sum[0]]}-${WORDS[sum[1]]}-${10 + (sum[2] % 90)}`;
}

/** validName is the shape of a machine name: two lowercase words of up to 12 letters and two digits. */
export const validName = (name) => typeof name === 'string' && /^[a-z]{1,12}-[a-z]{1,12}-[0-9]{2}$/.test(name);
