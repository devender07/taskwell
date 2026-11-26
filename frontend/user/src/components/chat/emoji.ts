// The emoji offered in chat, one per line with the words that find it in the picker's search.
export const EMOJI = `👍 thumbs up like yes ok good agree
❤️ red heart love
😂 joy laugh tears lol funny
🎉 party tada celebrate congrats
😮 wow surprised open mouth
😢 cry sad tear
👀 eyes look see watching
✅ check done yes complete tick
😀 grin smile happy
😃 smile happy big eyes
😄 smile happy laugh
😁 beam grin teeth
😆 laugh squint haha
😅 sweat smile relief nervous
🤣 rofl rolling laugh lol
🙂 slight smile
😉 wink
😊 blush smile happy
😇 angel halo innocent
🥰 love hearts adore
😍 heart eyes love
🤩 star struck amazing wow
😘 kiss blow
😋 yum tasty delicious
😛 tongue
😜 wink tongue joke
🤪 zany crazy silly
🤗 hug
🤭 oops giggle hand over mouth
🤔 thinking hmm
🤐 zipper mouth quiet secret
😐 neutral meh
😑 expressionless
😶 no mouth speechless
😏 smirk
😒 unamused annoyed
🙄 eye roll
😬 grimace awkward
😌 relieved calm
😔 pensive sad
😪 sleepy
😴 sleep zzz tired
😷 mask sick
🤒 sick fever thermometer
🤕 hurt bandage injured
🤢 nauseated sick gross
🥵 hot heat
🥶 cold freezing
😵 dizzy
🤯 mind blown exploding head
🤠 cowboy
🥳 party celebrate birthday
😎 cool sunglasses
🤓 nerd glasses
😕 confused
😟 worried
🙁 frown sad
😯 hushed surprised
😲 astonished shocked
😳 flushed embarrassed
🥺 pleading please puppy
😭 sob cry loud
😱 scream scared shock
😤 huff triumph frustrated
😡 angry rage mad
😠 angry mad
🤬 cursing swearing angry
💀 skull dead
👻 ghost
🤖 robot bot
💩 poop
👎 thumbs down dislike no bad
👌 ok perfect
✌️ victory peace
🤞 fingers crossed luck hope
🤟 love you
🤘 rock horns
🤙 call me shaka
👈 point left
👉 point right
👆 point up
👇 point down
✋ hand stop high five
👋 wave hello hi bye
👏 clap applause bravo
🙌 raised hands hooray praise
🙏 pray thanks please
🤝 handshake deal agree
💪 muscle strong flex
✍️ writing write
🧡 orange heart
💛 yellow heart
💚 green heart
💙 blue heart
💜 purple heart
🖤 black heart
🤍 white heart
💔 broken heart
💯 hundred perfect
🔥 fire lit hot
✨ sparkles shiny new
⭐ star
🎊 confetti celebrate
🎁 gift present
🏆 trophy win award
🚀 rocket ship launch
❌ cross no wrong x
⚠️ warning caution
❓ question
❗ exclamation important
💡 idea bulb light
📌 pin pushpin
📎 paperclip attach
🐛 bug
☕ coffee tea break
🍕 pizza food lunch
🍺 beer drink cheers`
  .split('\n')
  .map((line) => ({ emoji: line.slice(0, line.indexOf(' ')), words: line.slice(line.indexOf(' ') + 1) }))

// The emoji whose words contain every word of the query.
export function searchEmoji(q: string) {
  const terms = q.toLowerCase().split(/\s+/).filter(Boolean)
  return EMOJI.filter((e) => terms.every((t) => e.words.includes(t)))
}
