import './steps.css';

const STEPS = [
  {
    title: '记录',
    desc: '改完代码运行 qegg commit，在 .changes/ 留一条变更记录，写清类型和升级级别。',
  },
  {
    title: '合并',
    desc: '合入主分支，qegg 汇总待发记录，算出版本号并更新 CHANGELOG。',
  },
  {
    title: '发布',
    desc: '发布到 registry、打 tag、创建 GitHub Release。中断可以续跑。',
  },
];

export function Steps() {
  return (
    <section className="qegg-steps">
      {STEPS.map((step) => (
        <div className="qegg-steps__item" key={step.title}>
          <h2 className="qegg-steps__name">{step.title}</h2>
          <p className="qegg-steps__desc">{step.desc}</p>
        </div>
      ))}
    </section>
  );
}
