# Corpus sources

Real-world and test-suite PDFs for `TestCorpus` (corpus_test.go), fetched 2026-11-16 from the
public sources below (GitHub tarballs of the default branch at that date, a shallow clone for Poppler;
pdf.js `.link` targets from the URL in the link file). Files that fail an invariant on current main are listed in
KNOWN_FAILURES.tsv. Basenames are kept as published: TestCorpus seeds its word choice from them.

| File | Source | Why |
|---|---|---|
| `annotations/annotation_unbalanced_restore.pdf` | mozilla/pdf.js `test/pdfs/annotation_unbalanced_restore.pdf` | annotations coverage: annot:Square |
| `annotations/issue12810.pdf` | mozilla/pdf.js `test/pdfs/issue12810.pdf` | fails: render-changed |
| `annotations/issue20453.pdf` | mozilla/pdf.js `test/pdfs/issue20453.pdf` | fails: under-removal |
| `annotations/test_2957_1.pdf` | pymupdf/PyMuPDF `tests/resources/test_2957_1.pdf` | fails: render-changed |
| `annotations/test_2957_2.pdf` | pymupdf/PyMuPDF `tests/resources/test_2957_2.pdf` | annotations coverage: annot:Redact |
| `annotations/test_3376.pdf` | pymupdf/PyMuPDF `tests/resources/test_3376.pdf` | fails: render-changed |
| `attachments/issue18030.pdf` | mozilla/pdf.js `test/pdfs/issue18030.pdf` | attachments coverage: annot:Link,embeddedfile,eof:2,objstm,producer:tex |
| `attachments/kept-no-fields.pdf` | qpdf/qpdf `qpdf/qtest/qpdf/kept-no-fields.pdf` | attachments coverage: embeddedfile |
| `attachments/richmedia_instance_asset.pdf` | gitlab.freedesktop.org/poppler/test `unittestcases/richmedia_instance_asset.pdf` | attachments coverage: embeddedfile,richmedia |
| `attachments/structure-attached.pdf` | corkami/pocs `pdf/structure-attached.pdf` | attachments coverage: annot:FileAttachment,embeddedfile |
| `attachments/test_annot_file_info.pdf` | pymupdf/PyMuPDF `tests/resources/test_annot_file_info.pdf` | attachments coverage: annot:FileAttachment,eof:2,objstm,producer:quartz,producer:tex |
| `attachments/veraPDF test suite 6-3-3-t01-fail-p.pdf` | veraPDF/veraPDF-corpus `PDF_A-2b/6.3 Annotations/6.3.3 Annotation appearances/veraPDF test suite 6-3-3-t01-fail-p.pdf` | attachments coverage: annot:FileAttachment |
| `cjk-vertical/VerticalText.pdf` | pdf-association/pdf-differences `VerticalText/VerticalText.pdf` | fails: over-removal,render-changed |
| `cjk-vertical/issue20930.pdf` | mozilla/pdf.js `test/pdfs/issue20930.pdf` | cjk-vertical coverage: identity-v |
| `cjk-vertical/issue6387.pdf` | mozilla/pdf.js `test/pdfs/issue6387.pdf` | cjk-vertical coverage: eof:2,identity-v,linearized,producer:adobe,smask |
| `cjk-vertical/veraPDF test suite 6-2-10-3-1-t01-pass-c.pdf` | veraPDF/veraPDF-corpus `PDF_A-4/6.2 Graphics/6.2.10 Fonts/6.2.10.3 Composite fonts/6.2.10.3.1 General/veraPDF test suite 6-2-10-3-1-t01-pass-c.pdf` | cjk-vertical coverage: identity-v,invisible-text,structtreeroot |
| `cjk-vertical/veraPDF test suite 6-2-11-3-1-t01-pass-c.pdf` | veraPDF/veraPDF-corpus `PDF_A-2b/6.2 Graphics/6.2.11 Fonts/6.2.11.3 Composite fonts/6.2.11.3.1 General/veraPDF test suite 6-2-11-3-1-t01-pass-c.pdf` | cjk-vertical coverage: actualtext,identity-v,invisible-text,structtreeroot |
| `cjk-vertical/vertical.pdf` | mozilla/pdf.js `test/pdfs/vertical.pdf` | cjk-vertical coverage: identity-v,producer:tex |
| `encrypted/Example - overlapped & protected text.pdf` | corkami/pocs `pdf/PDFSecrets/Example - overlapped & protected text.pdf` | fails: render-changed |
| `encrypted/PasswordEncryptedReconstructed.pdf` | gitlab.freedesktop.org/poppler/test `unittestcases/PasswordEncryptedReconstructed.pdf` | encrypted coverage: encrypt,eof:2 |
| `encrypted/isartor-6-1-3-t02-fail-a.pdf` | veraPDF/veraPDF-corpus `Isartor test files/PDFA-1b/6.1 File structure/6.1.3 File trailer/isartor-6-1-3-t02-fail-a.pdf` | encrypted coverage: encrypt |
| `encrypted/issue19484_1.pdf` | mozilla/pdf.js `test/pdfs/issue19484_1.pdf` | fails: scrub-residue |
| `encrypted/issue19484_2.pdf` | mozilla/pdf.js `test/pdfs/issue19484_2.pdf` | fails: scrub-residue |
| `encrypted/issue6010_1.pdf` | mozilla/pdf.js `test/pdfs/issue6010_1.pdf` | encrypted coverage: encrypt |
| `forms/annotation-polyline-polygon.pdf` | mozilla/pdf.js `test/pdfs/annotation-polyline-polygon.pdf` | fails: actualtext-survived,text-survived,under-removal |
| `forms/bug1722036.pdf` | https://bugzilla.mozilla.org/attachment.cgi?id=9232845 (pdf.js test/pdfs .link target) | leak: orphan widgets with /TU text |
| `forms/cms-etc-filled.pdf` | pymupdf/PyMuPDF `tests/resources/cms-etc-filled.pdf` | fails: text-moved |
| `forms/issue11131_reduced.pdf` | mozilla/pdf.js `test/pdfs/issue11131_reduced.pdf` | fails: scrub-residue |
| `forms/issue1998.pdf` | https://web.archive.org/web/20150223193848/http://www.uctc.net/access/30/Access%2030%20-%2002%20-%20Horse%20Power.pdf (pdf.js test/pdfs .link target) | fails: apply-error |
| `forms/issue9940.pdf` | mozilla/pdf.js `test/pdfs/issue9940.pdf` | fails: text-moved |
| `forms/shared-unnamed-field.pdf` | qpdf/qpdf `qpdf/qtest/qpdf/shared-unnamed-field.pdf` | fails: scrub-residue |
| `forms/test_2645_1.pdf` | pymupdf/PyMuPDF `tests/resources/test_2645_1.pdf` | fails: render-changed |
| `forms/test_3950.pdf` | pymupdf/PyMuPDF `tests/resources/test_3950.pdf` | fails: render-changed |
| `forms/test_4546.pdf` | pymupdf/PyMuPDF `tests/resources/test_4546.pdf` | fails: actualtext-survived |
| `incremental/PDF-versions1.pdf` | pdf-association/pdf-differences `PDF-version/PDF-versions1.pdf` | incremental coverage: eof:2 |
| `incremental/incremental-1.pdf` | qpdf/qpdf `qpdf/qtest/qpdf/incremental-1.pdf` | leak: old content kept by trailer /Gone after an incremental update |
| `incremental/incremental-3.pdf` | qpdf/qpdf `qpdf/qtest/qpdf/incremental-3.pdf` | incremental coverage: eof:2 |
| `incremental/issue17554.pdf` | mozilla/pdf.js `test/pdfs/issue17554.pdf` | incremental coverage: eof:2 |
| `incremental/quine.pdf` | corkami/pocs `pdf/external/quine.pdf` | fails: over-removal,render-changed,text-moved,under-removal |
| `incremental/structure-ignoredeof.pdf` | corkami/pocs `pdf/structure-ignoredeof.pdf` | incremental coverage: eof:3 |
| `incremental/test_4564.pdf` | pymupdf/PyMuPDF `tests/resources/test_4564.pdf` | incremental coverage: eof:2 |
| `javascript/CompactedPDFSyntaxTest.pdf` | pdf-association/safedocs `CompactedSyntax/CompactedPDFSyntaxTest.pdf` | fails: render-changed |
| `javascript/UseAttachments.pdf` | gitlab.freedesktop.org/poppler/test `unittestcases/UseAttachments.pdf` | javascript coverage: eof:2,javascript,js,producer:quartz,producer:tex |
| `javascript/enc-XI-attachments-base.pdf` | qpdf/qpdf `qpdf/qtest/qpdf/enc-XI-attachments-base.pdf` | javascript coverage: eof:2,image,javascript,js,linearized,objstm |
| `javascript/isartor-6-6-1-t03-fail-f.pdf` | veraPDF/veraPDF-corpus `Isartor test files/PDFA-1b/6.6 Actions/6.6.1 General/isartor-6-6-1-t03-fail-f.pdf` | javascript coverage: javascript,js |
| `javascript/test_3863.pdf.pdf.4.png` | pymupdf/PyMuPDF `tests/resources/test_3863.pdf.pdf.4.png` | javascript coverage: js |
| `javascript/txt2pdf.pdf` | https://web.archive.org/web/20150910131256/http://www.sanface.com/pdf/test.pdf (pdf.js test/pdfs .link target) | javascript coverage: annot:Link,launch,uri |
| `javascript/veraPDF test suite 6-6-1-t01-fail-a.pdf` | veraPDF/veraPDF-corpus `PDF_A-1b/6.6 Actions/6.6.1 General/veraPDF test suite 6-6-1-t01-fail-a.pdf` | Hide action |
| `large-linky/bug1978317.pdf` | mozilla/pdf.js `test/pdfs/bug1978317.pdf` | large-linky coverage: annot:Link,objstm,structtreeroot,uri,uri:1000+ |
| `layers/NestedLayers.pdf` | gitlab.freedesktop.org/poppler/test `unittestcases/NestedLayers.pdf` | layers: hidden layer text survives without ShowAllLayers |
| `layers/hidden_oc_form_cm.pdf` | mozilla/pdf.js `test/pdfs/hidden_oc_form_cm.pdf` | layers coverage: oc-off,ocproperties |
| `layers/issue15716.pdf` | mozilla/pdf.js `test/pdfs/issue15716.pdf` | fails: actualtext-survived,under-removal |
| `layers/issue15803.pdf` | https://github.com/mozilla/pdf.js/files/10200431/ocg.pdf (pdf.js test/pdfs .link target) | layers coverage: annot:Square,ocproperties |
| `layers/issue19624.pdf` | https://github.com/user-attachments/files/19126771/okm2500682934750615600.pdf (pdf.js test/pdfs .link target) | fails: actualtext-survived,over-removal,text-survived,under-removal |
| `layers/issue9949.pdf` | mozilla/pdf.js `test/pdfs/issue9949.pdf` | fails: over-removal |
| `layers/test-3143.pdf` | pymupdf/PyMuPDF `tests/resources/test-3143.pdf` | layers coverage: ocproperties,rotated |
| `layers/vis_policy_test.pdf` | gitlab.freedesktop.org/poppler/test `unittestcases/vis_policy_test.pdf` | layers: visibility policies (OCMD) hide text |
| `malformed/3.pdf` | qpdf/qpdf `examples/qtest/bookmarks/3.pdf` | malformed coverage:  |
| `malformed/adobe-tiny.pdf` | corkami/pocs `pdf/adobe-tiny.pdf` | malformed coverage:  |
| `malformed/bad.pdf` | qpdf/qpdf `examples/qtest/extend-c-api/bad.pdf` | malformed coverage:  |
| `malformed/bad1.pdf` | qpdf/qpdf `qpdf/qtest/qpdf/bad1.pdf` | malformed coverage:  |
| `malformed/bad22.pdf` | qpdf/qpdf `qpdf/qtest/qpdf/bad22.pdf` | leak: old content kept by a non-standard trailer key |
| `malformed/issue-335b.pdf` | qpdf/qpdf `qpdf/qtest/qpdf/issue-335b.pdf` | malformed coverage:  |
| `malformed/issue18503.pdf` | https://github.com/user-attachments/files/16402842/apl_23_003.pdf (pdf.js test/pdfs .link target) | leak: repaired file, redaction not applied (bulk5 regression) |
| `malformed/poppler-85140-0.pdf` | mozilla/pdf.js `test/pdfs/poppler-85140-0.pdf` | malformed coverage:  |
| `misc/DefaultRGBColourSpaces-inherit.pdf` | pdf-association/pdf-differences `DefaultColorSpaces/DefaultRGBColourSpaces-inherit.pdf` | fails: render-changed |
| `misc/DefaultRGBColourSpaces.pdf` | pdf-association/pdf-differences `DefaultColorSpaces/DefaultRGBColourSpaces.pdf` | fails: render-changed |
| `misc/Dialect-ContentStreamsViaResourceNames.pdf` | pdf-association/safedocs `Dialects/Dialect-ContentStreamsViaResourceNames.pdf` | fails: render-changed |
| `misc/Dialect-ContentStreamsWithIndirectRefs.pdf` | pdf-association/safedocs `Dialects/Dialect-ContentStreamsWithIndirectRefs.pdf` | fails: render-changed |
| `misc/GHOSTSCRIPT-698804-1-fuzzed.pdf` | mozilla/pdf.js `test/pdfs/GHOSTSCRIPT-698804-1-fuzzed.pdf` | fails: page-count-changed |
| `misc/NegativeFontSize.pdf` | pdf-association/pdf-differences `NegativeFontSize/NegativeFontSize.pdf` | fails: over-removal,render-changed,text-moved |
| `misc/TextClippingModeChanges.pdf` | pdf-association/pdf-differences `TextClipModeChanges/TextClippingModeChanges.pdf` | fails: render-changed |
| `misc/ThuluthFeatures.pdf` | mozilla/pdf.js `test/pdfs/ThuluthFeatures.pdf` | fails: render-changed |
| `misc/bad-xref-entry.pdf` | qpdf/qpdf `qpdf/qtest/qpdf/bad-xref-entry.pdf` | fails: actualtext-survived,text-survived,under-removal |
| `misc/bug1873345.pdf` | mozilla/pdf.js `test/pdfs/bug1873345.pdf` | fails: render-changed |
| `misc/bug2021583.pdf` | mozilla/pdf.js `test/pdfs/bug2021583.pdf` | fails: over-removal |
| `misc/bug868745.pdf` | mozilla/pdf.js `test/pdfs/bug868745.pdf` | fails: render-changed |
| `misc/bug911034.pdf` | mozilla/pdf.js `test/pdfs/bug911034.pdf` | fails: render-changed |
| `misc/bug946506.pdf` | mozilla/pdf.js `test/pdfs/bug946506.pdf` | fails: over-removal,render-changed |
| `misc/extgstate.pdf` | mozilla/pdf.js `test/pdfs/extgstate.pdf` | fails: over-removal,render-changed |
| `misc/form-xobjects-no-resources-out.pdf` | qpdf/qpdf `qpdf/qtest/qpdf/form-xobjects-no-resources-out.pdf` | fails: over-removal,render-changed |
| `misc/form-xobjects-no-resources.pdf` | qpdf/qpdf `qpdf/qtest/qpdf/form-xobjects-no-resources.pdf` | fails: over-removal,render-changed |
| `misc/form-xobjects-some-resources1-out.pdf` | qpdf/qpdf `qpdf/qtest/qpdf/form-xobjects-some-resources1-out.pdf` | fails: over-removal,render-changed |
| `misc/form-xobjects-some-resources1.pdf` | qpdf/qpdf `qpdf/qtest/qpdf/form-xobjects-some-resources1.pdf` | fails: over-removal,render-changed |
| `misc/form-xobjects-some-resources2-out.pdf` | qpdf/qpdf `qpdf/qtest/qpdf/form-xobjects-some-resources2-out.pdf` | fails: over-removal,render-changed,text-moved |
| `misc/form-xobjects-some-resources2.pdf` | qpdf/qpdf `qpdf/qtest/qpdf/form-xobjects-some-resources2.pdf` | fails: over-removal,render-changed,text-moved |
| `misc/issue1055r.pdf` | mozilla/pdf.js `test/pdfs/issue1055r.pdf` | fails: render-changed |
| `misc/issue13211.pdf` | mozilla/pdf.js `test/pdfs/issue13211.pdf` | fails: under-removal |
| `misc/issue18768_initial_narrow_gap.pdf` | mozilla/pdf.js `test/pdfs/issue18768_initial_narrow_gap.pdf` | fails: over-removal |
| `misc/issue18986.pdf` | mozilla/pdf.js `test/pdfs/issue18986.pdf` | fails: page-count-changed |
| `misc/issue19326.pdf` | mozilla/pdf.js `test/pdfs/issue19326.pdf` | fails: render-changed |
| `misc/issue1940.pdf` | https://web.archive.org/web/20120105135646/http://www.parallax.com/dl/docs/prod/acc/memsickit.pdf (pdf.js test/pdfs .link target) | fails: over-removal,render-changed |
| `misc/issue19550.pdf` | mozilla/pdf.js `test/pdfs/issue19550.pdf` | fails: over-removal |
| `misc/issue19800.pdf` | mozilla/pdf.js `test/pdfs/issue19800.pdf` | fails: apply-error |
| `misc/issue19802.pdf` | mozilla/pdf.js `test/pdfs/issue19802.pdf` | fails: over-removal,render-changed |
| `misc/issue22489.pdf` | mozilla/pdf.js `test/pdfs/issue22489.pdf` | fails: actualtext-survived,under-removal |
| `misc/issue5801.pdf` | mozilla/pdf.js `test/pdfs/issue5801.pdf` | fails: render-changed |
| `misc/issue6413.pdf` | mozilla/pdf.js `test/pdfs/issue6413.pdf` | fails: render-changed |
| `misc/issue7406.pdf` | mozilla/pdf.js `test/pdfs/issue7406.pdf` | fails: render-changed |
| `misc/melissa.pdf` | corkami/pocs `pdf/nes/melissa.pdf` | fails: render-changed |
| `misc/operator_list_cycle.pdf` | mozilla/pdf.js `test/pdfs/operator_list_cycle.pdf` | fails: timeout |
| `misc/pattern_text_non_embedded_font.pdf` | mozilla/pdf.js `test/pdfs/pattern_text_non_embedded_font.pdf` | fails: render-changed |
| `misc/perf_structure_forms.pdf` | mozilla/pdf.js `test/pdfs/perf_structure_forms.pdf` | fails: over-removal |
| `misc/perf_text_render_modes.pdf` | mozilla/pdf.js `test/pdfs/perf_text_render_modes.pdf` | fails: render-changed |
| `misc/perf_text_transformed.pdf` | mozilla/pdf.js `test/pdfs/perf_text_transformed.pdf` | fails: over-removal,render-changed,text-moved |
| `misc/perf_transparency_group_stacks.pdf` | mozilla/pdf.js `test/pdfs/perf_transparency_group_stacks.pdf` | fails: actualtext-survived,under-removal |
| `misc/perf_transparency_soft_masks.pdf` | mozilla/pdf.js `test/pdfs/perf_transparency_soft_masks.pdf` | fails: actualtext-survived,under-removal |
| `misc/poppler-67295-0.pdf` | mozilla/pdf.js `test/pdfs/poppler-67295-0.pdf` | fails: add-redaction-error |
| `misc/test_2645_2.pdf` | pymupdf/PyMuPDF `tests/resources/test_2645_2.pdf` | fails: over-removal,render-changed |
| `misc/test_3072.pdf` | pymupdf/PyMuPDF `tests/resources/test_3072.pdf` | fails: over-removal,render-changed,text-moved |
| `misc/test_3650.pdf` | pymupdf/PyMuPDF `tests/resources/test_3650.pdf` | fails: over-removal |
| `misc/test_4412.pdf` | pymupdf/PyMuPDF `tests/resources/test_4412.pdf` | leak: redacted text in pages outside the page tree (orphan pages) |
| `misc/test_4699.pdf` | pymupdf/PyMuPDF `tests/resources/test_4699.pdf` | fails: render-changed |
| `misc/test_4944.pdf` | pymupdf/PyMuPDF `tests/resources/test_4944.pdf` | fails: under-removal |
| `misc/tiling-pattern-tiny-steps-large.pdf` | mozilla/pdf.js `test/pdfs/tiling-pattern-tiny-steps-large.pdf` | fails: timeout |
| `misc/tiling-pattern-tiny-steps.pdf` | mozilla/pdf.js `test/pdfs/tiling-pattern-tiny-steps.pdf` | fails: timeout |
| `misc/veraPDF test suite 6-2-10-4-1-t02-fail-a.pdf` | veraPDF/veraPDF-corpus `PDF_A-4/6.2 Graphics/6.2.10 Fonts/6.2.10.4 Embedding/6.2.10.4.1 General/veraPDF test suite 6-2-10-4-1-t02-fail-a.pdf` | fails: under-removal |
| `misc/veraPDF test suite 6-2-11-4-1-t02-fail-a.pdf` | veraPDF/veraPDF-corpus `PDF_A-2b/6.2 Graphics/6.2.11 Fonts/6.2.11.4 Embedding/6.2.11.4.1 General/veraPDF test suite 6-2-11-4-1-t02-fail-a.pdf` | fails: under-removal |
| `misc/xref-compressed-in-compressed.pdf` | qpdf/qpdf `qpdf/qtest/qpdf/xref-compressed-in-compressed.pdf` | fails: over-removal |
| `office-export/UseNone.pdf` | gitlab.freedesktop.org/poppler/test `unittestcases/UseNone.pdf` | office-export coverage: producer:quartz,producer:tex |
| `office-export/empty-stream-compressed.pdf` | qpdf/qpdf `qpdf/qtest/qpdf/empty-stream-compressed.pdf` | office-export coverage: producer:chrome |
| `office-export/empty-stream-uncompressed.pdf` | qpdf/qpdf `qpdf/qtest/qpdf/empty-stream-uncompressed.pdf` | office-export coverage: producer:chrome |
| `office-export/issue15577.pdf` | https://github.com/mozilla/pdf.js/files/9813960/hang-080214-152005-90.pdf (pdf.js test/pdfs .link target) | office-export coverage: producer:msoffice |
| `office-export/issue15690.pdf` | mozilla/pdf.js `test/pdfs/issue15690.pdf` | office-export coverage: producer:msoffice |
| `office-export/test_4571.pdf` | pymupdf/PyMuPDF `tests/resources/test_4571.pdf` | office-export coverage: producer:libreoffice |
| `rotated/bug867484.pdf` | https://bugzilla.mozilla.org/attachment.cgi?id=744001 (pdf.js test/pdfs .link target) | rotated coverage: eof:2,image,linearized,objstm,producer:adobe,producer:msoffice,rotated,smask,str |
| `rotated/inherited-rotate.pdf` | qpdf/qpdf `qpdf/qtest/qpdf/inherited-rotate.pdf` | rotated coverage: rotated |
| `rotated/issue6019.pdf` | mozilla/pdf.js `test/pdfs/issue6019.pdf` | rotated coverage: rotated |
| `rotated/issue8125.pdf` | mozilla/pdf.js `test/pdfs/issue8125.pdf` | rotated coverage: rotated |
| `rotated/minimal-rotated.pdf` | qpdf/qpdf `qpdf/qtest/qpdf/minimal-rotated.pdf` | rotated coverage: rotated |
| `rotated/test-2812.pdf` | pymupdf/PyMuPDF `tests/resources/test-2812.pdf` | rotated coverage: rotated |
| `scanned-ocr/alphatrans.pdf` | mozilla/pdf.js `test/pdfs/alphatrans.pdf` | scanned-ocr coverage: annot:Link,image,invisible-text,pattern,uri |
| `scanned-ocr/issue4244.pdf` | https://web.archive.org/web/20180613082417/https://tcpdf.org/files/examples/example_026.pdf (pdf.js test/pdfs .link target) | scanned-ocr coverage: annot:Link,image,invisible-text,uri |
| `scanned-ocr/issue9650.pdf` | https://github.com/mozilla/pdf.js/files/1898753/Kred.Eingangsrechnungen.pdf (pdf.js test/pdfs .link target) | fails: render-changed |
| `scanned-ocr/numeric-and-string-2.pdf` | qpdf/qpdf `qpdf/qtest/qpdf/numeric-and-string-2.pdf` | scanned-ocr coverage: image,invisible-text |
| `scanned-ocr/test_4670.pdf` | pymupdf/PyMuPDF `tests/resources/test_4670.pdf` | scanned-ocr coverage: image,invisible-text,producer:scanner |
| `scanned-ocr/test_4790.pdf` | pymupdf/PyMuPDF `tests/resources/test_4790.pdf` | fails: actualtext-survived,text-survived,under-removal |
| `tagged/8.2.4-t03-fail-a.pdf` | veraPDF/veraPDF-corpus `PDF_UA-2/8.2 Logical structure/8.2.4 Structure types/8.2.4-t03-fail-a.pdf` | fails: render-changed |
| `tagged/8.2.4-t03-fail-b.pdf` | veraPDF/veraPDF-corpus `PDF_UA-2/8.2 Logical structure/8.2.4 Structure types/8.2.4-t03-fail-b.pdf` | fails: render-changed |
| `tagged/8.2.4-t03-pass-a.pdf` | veraPDF/veraPDF-corpus `PDF_UA-2/8.2 Logical structure/8.2.4 Structure types/8.2.4-t03-pass-a.pdf` | fails: render-changed |
| `tagged/8.2.5.29-t01-fail-a.pdf` | veraPDF/veraPDF-corpus `PDF_UA-2/8.2 Logical structure/8.2.5 Additional requirements for specific structure types/8.2.5.29 Formula/8.2.5.29-t01-fail-a.pdf` | fails: render-changed |
| `tagged/8.2.5.29-t01-pass-a.pdf` | veraPDF/veraPDF-corpus `PDF_UA-2/8.2 Logical structure/8.2.5 Additional requirements for specific structure types/8.2.5.29 Formula/8.2.5.29-t01-pass-a.pdf` | fails: render-changed |
| `tagged/issue12909.pdf` | https://web.archive.org/web/20210126084513/https://www.machonso.org/uploads/images/%D7%95%D7%95%D7%9C%D7%A3.pdf (pdf.js test/pdfs .link target) | fails: over-removal |
| `tagged/test_4363.pdf` | pymupdf/PyMuPDF `tests/resources/test_4363.pdf` | fails: actualtext-survived,render-changed |
| `tagged/test_4907.pdf` | pymupdf/PyMuPDF `tests/resources/test_4907.pdf` | fails: actualtext-crash |
| `tex/bug1539074.1.pdf` | mozilla/pdf.js `test/pdfs/bug1539074.1.pdf` | fails: render-changed |
| `tex/fuzz-16214.pdf` | qpdf/qpdf `qpdf/qtest/qpdf/fuzz-16214.pdf` | tex coverage: image,objstm,producer:adobe,producer:tex |
| `tex/issue13845.pdf` | https://github.com/mozilla/pdf.js/files/6916129/example.pdf (pdf.js test/pdfs .link target) | tex coverage: producer:tex |
| `tex/issue8111.pdf` | mozilla/pdf.js `test/pdfs/issue8111.pdf` | tex coverage: pattern,producer:tex |
| `tex/test_4712_b.pdf` | pymupdf/PyMuPDF `tests/resources/test_4712_b.pdf` | tex coverage: producer:tex |
| `tex/xr01.pdf` | gitlab.freedesktop.org/poppler/test `unittestcases/xr01.pdf` | tex coverage: annot:Link,producer:tex |
| `type3/bug1011159.pdf` | mozilla/pdf.js `test/pdfs/bug1011159.pdf` | fails: render-changed,under-removal |
| `type3/bug1245391_reduced.pdf` | mozilla/pdf.js `test/pdfs/bug1245391_reduced.pdf` | fails: render-changed |
| `type3/issue14953.pdf` | mozilla/pdf.js `test/pdfs/issue14953.pdf` | fails: render-changed,under-removal |
| `type3/issue14999_reduced.pdf` | mozilla/pdf.js `test/pdfs/issue14999_reduced.pdf` | fails: render-changed |
| `type3/issue6605.pdf` | mozilla/pdf.js `test/pdfs/issue6605.pdf` | fails: under-removal |
| `type3/pr4922.pdf` | mozilla/pdf.js `test/pdfs/pr4922.pdf` | fails: render-changed |
| `xfa/formevent_js.pdf` | corkami/pocs `pdf/formevent_js.pdf` | xfa coverage: acroform,annot:Widget,xfa |
| `xfa/fw9.pdf` | https://www.irs.gov/pub/irs-pdf/fw9.pdf | xfa coverage: acroform,annot:Widget,byterange,embeddedfile,eof:3,javascript,js,linearized,objs |
| `xfa/test-4055.pdf` | pymupdf/PyMuPDF `tests/resources/test-4055.pdf` | xfa coverage: acroform,annot:Widget,byterange,embeddedfile,eof:3,javascript,js,linearized,objs |
| `xfa/veraPDF test suite 6-4-2-t01-fail-a.pdf` | veraPDF/veraPDF-corpus `PDF_A-4/6.4 Interactive forms/6.4.2 XFA forms/veraPDF test suite 6-4-2-t01-fail-a.pdf` | xfa coverage: acroform,embeddedfile,xfa |
| `xfa/xfa_issue13584.pdf` | https://web.archive.org/web/20210518165046/http://education.ohio.gov/getattachment/Topics/Special-Education/Preschool-Special-Education/ETR-Preschool-Planning-Form.pdf.aspx (pdf.js test/pdfs .link target) | xfa coverage: acroform,byterange,embeddedfile,javascript,js,objstm,producer:adobe,structtreero |
| `xfa/xfa_issue22023.pdf` | mozilla/pdf.js `test/pdfs/xfa_issue22023.pdf` | xfa coverage: acroform,xfa |
