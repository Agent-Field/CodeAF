import { SectionHeading, Text } from '../../../components/ui';
import { PreviewButtons, PreviewCard, PreviewError, PreviewField, PreviewShot, PreviewState, PreviewText } from '../preview/PreviewCard';
import '../preview/preview.css';
import '../preview/preview-specimen.css';

const noop = () => {};

/**
 * Every hover-preview card of Shell 3k, drawn with the same pieces the live tabs use but with fixed sample
 * content (specimen only: nothing here is a live session). It follows the page's theme, so it reads in light and dark.
 */
export function PreviewSpecimen() {
  return <section className="preview-specimen" data-preview-specimen aria-label="Tab hover preview specimen">
    <SectionHeading>Tab hover preview</SectionHeading>
    <Text>Specimen. 300px text cards: kind, state, title, then the one piece that matters. Web pages alone get a screenshot. A card that needs you carries its primary action.</Text>
    <div className="preview-specimen-grid">
      <PreviewCard kind="conversation" title="Config stack" state={<PreviewState dot="accent">4 running</PreviewState>}>
        <PreviewText>Last reply: “The fixtures and the v1 port run now, and the changelog waits on the fixtures.”</PreviewText>
      </PreviewCard>
      <PreviewCard kind="task" lead="amber" title="Port fix to v1 branch" state="Needs you" actions={<PreviewButtons primary={{ label: 'Allow all', onClick: noop }} secondary={{ label: 'Review', onClick: noop }}/>}>
        <PreviewText>Allow 3 git actions?</PreviewText>
      </PreviewCard>
      <PreviewCard kind="task" lead="amber" title="Port fix to v1 branch" state="Needs you" actions={<PreviewButtons primary={{ label: 'Allow all', onClick: noop }} secondary={{ label: 'Review', onClick: noop }}/>}>
        <PreviewText>Allow 3 git actions?</PreviewText>
        <PreviewError>Not sent. The engine did not answer.</PreviewError>
      </PreviewCard>
      <PreviewCard kind="terminal" title="nightly-bench" state="exit 0 · 2m ago">
        <PreviewField label="Last output" lines={[{ text: 'BenchmarkLex-10      812 ns/op' }, { text: 'BenchmarkParse-10   2104 ns/op' }, { text: 'ok  codeaf/internal/parse  4.2s', tone: 'ink' }]}/>
      </PreviewCard>
      <PreviewCard kind="diff" title="lexer.go" state={<><span className="preview-diff-add">+12</span><span className="preview-diff-del">−3</span></>}>
        <PreviewField label="First changes" lines={[{ text: '− return Token{Kind: Comma}', tone: 'del' }, { text: '+ if l.peekClose() && !l.strict {', tone: 'add' }]}/>
      </PreviewCard>
      <PreviewCard kind="file" title="lexer.go" state="412 lines">
        <PreviewField label="First lines" lines={[{ text: 'package parse' }, { text: 'import "unicode/utf8"' }, { text: 'type Lexer struct {' }]}/>
      </PreviewCard>
      <PreviewShot title="encoding/json" address="pkg.go.dev/encoding/json" heading="encoding/json" excerpt="Package json implements encoding and decoding of JSON as defined in RFC 7159."/>
      <PreviewCard kind="conversation" lead="danger" title="Migrate fixtures" state="Failed">
        <PreviewText>Last reply: “The v1 fixtures do not load; the loader rejects the trailing comma.”</PreviewText>
      </PreviewCard>
      <PreviewCard kind="settings" title="Models"/>
      <PreviewCard kind="history" title="History"/>
    </div>
  </section>;
}
